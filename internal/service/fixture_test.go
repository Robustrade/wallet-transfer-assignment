package service_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type fixture struct {
	t           *testing.T
	ctx         context.Context
	db          *gorm.DB
	svc         *service.TransferService
	dsn         string
	application string
}

const testMaxTransferAmount int64 = 1_000_000_000_000

// setup gives each test its own small schema in a dedicated *_test database.
func setup(t *testing.T) fixture {
	t.Helper()
	if testing.Short() {
		t.Skip("PostgreSQL integration test")
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run transfer integration tests")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("test DSN must differ from application DSN")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("TEST_DATABASE_URL must name a dedicated PostgreSQL *_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin := openTestDB(t, ctx, dsn)
	var database string
	if err := admin.WithContext(ctx).Raw("SELECT current_database()").Row().Scan(&database); err != nil || !strings.HasSuffix(database, "_test") {
		t.Fatal("refusing non-test database")
	}
	schema := fmt.Sprintf("transfer_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if err := admin.WithContext(ctx).Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := admin.WithContext(cleanupCtx).Exec("DROP SCHEMA " + quotedSchema + " CASCADE").Error; err != nil {
			t.Errorf("clean test schema: %v", err)
		}
	})
	query := u.Query()
	query.Set("search_path", schema)
	query.Set("application_name", schema)
	u.RawQuery = query.Encode()
	db := openTestDB(t, ctx, u.String())
	name := "000001_initial.up.sql"
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply %s: %v", name, err)
	}
	f := fixture{t: t, ctx: ctx, db: db, svc: service.NewTransferService(db, 5*time.Second, testMaxTransferAmount), dsn: u.String(), application: schema}
	f.exec("INSERT INTO wallets(id,opening_balance,balance) VALUES ('wallet_1',1000,1000),('wallet_2',500,500)")
	return f
}

func openTestDB(t *testing.T, ctx context.Context, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal("open test database failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatal("test database unavailable")
	}
	return db
}

// pending simulates a process stopping after its durable reservation committed.
func (f fixture) pending(key string, amount int64, from, to string) string {
	f.t.Helper()
	tx := f.db.WithContext(f.ctx).Begin()
	if tx.Error != nil {
		f.t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()
	var id string
	if err := tx.Raw("INSERT INTO transfers(from_wallet_id,to_wallet_id,amount) VALUES(?,?,?) RETURNING id", from, to, amount).Row().Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	request := domain.TransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
	if err := tx.Exec("INSERT INTO idempotency_records(idempotency_key,request_hash,transfer_id) VALUES(?,?,?)", key, request.Hash(), id).Error; err != nil {
		f.t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f fixture) mustProcess(id string) service.TransferResult {
	f.t.Helper()
	result, err := f.svc.ProcessTransfer(f.ctx, id)
	if err != nil {
		f.t.Fatalf("process transfer: %v", err)
	}
	return result
}

func (f fixture) exec(query string, args ...any) {
	f.t.Helper()
	if err := f.db.WithContext(f.ctx).Exec(query, args...).Error; err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) wantBalance(id string, want int64) {
	f.t.Helper()
	var got int64
	if err := f.db.WithContext(f.ctx).Raw("SELECT balance FROM wallets WHERE id=?", id).Row().Scan(&got); err != nil {
		f.t.Fatal(err)
	}
	if got != want {
		f.t.Fatalf("balance %s = %d, want %d", id, got, want)
	}
}

func (f fixture) wantCount(table string, want int64) {
	f.t.Helper()
	var got int64
	if err := f.db.WithContext(f.ctx).Table(table).Count(&got).Error; err != nil {
		f.t.Fatal(err)
	}
	if got != want {
		f.t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}

func (f fixture) wantState(id string, want domain.TransferStatus) {
	f.t.Helper()
	var got domain.TransferStatus
	if err := f.db.WithContext(f.ctx).Raw("SELECT status FROM transfers WHERE id=?", id).Row().Scan(&got); err != nil {
		f.t.Fatal(err)
	}
	if got != want {
		f.t.Fatalf("state = %s, want %s", got, want)
	}
}

func (f fixture) wantSnapshot(id string, want service.TransferResult) {
	f.t.Helper()
	var status int
	var body string
	if err := f.db.WithContext(f.ctx).Raw("SELECT response_status,response_body FROM idempotency_records WHERE transfer_id=?", id).Row().Scan(&status, &body); err != nil {
		f.t.Fatal(err)
	}
	if status != want.StatusCode || body != string(want.Body) {
		f.t.Fatalf("snapshot %d %s differs from result %d %s", status, body, want.StatusCode, want.Body)
	}
}

func (f fixture) wantPending(id string) {
	f.t.Helper()
	f.wantState(id, domain.Pending)
	var saved bool
	if err := f.db.WithContext(f.ctx).Raw(
		"SELECT response_status IS NOT NULL OR response_body IS NOT NULL FROM idempotency_records WHERE transfer_id=?", id,
	).Row().Scan(&saved); err != nil {
		f.t.Fatal(err)
	}
	if saved {
		f.t.Fatal("pending transfer must not retain a terminal response")
	}
}
