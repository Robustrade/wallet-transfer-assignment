//go:build integration

// Integration tests for the PostgreSQL repositories and the transactional
// transfer workflow. Excluded from the default build; run with:
//
//	go test ./... -tags=integration
//
// Requires a reachable PostgreSQL. Set TEST_DATABASE_URL or rely on the default
// local DSN. Tests that cannot connect are skipped, not failed.
package postgres_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/platform/id"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/migrations"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

func setup(t *testing.T) (*postgres.DB, service.Repositories) {
	t.Helper()
	ctx := context.Background()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	db, err := postgres.Connect(ctx, dsn, 10, 3*time.Second)
	if err != nil {
		t.Skipf("no test database reachable: %v", err)
	}
	t.Cleanup(db.Close)

	if err := postgres.NewMigrator(db, migrations.FS).Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	truncate(t, db.Pool())

	repos := service.Repositories{
		Wallets:     postgres.NewWalletRepository(db),
		Transfers:   postgres.NewTransferRepository(db),
		Ledger:      postgres.NewLedgerRepository(db),
		Idempotency: postgres.NewIdempotencyRepository(db),
	}
	return db, repos
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func seedWallet(t *testing.T, pool *pgxpool.Pool, walletID string, balance int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance, currency) VALUES ($1, $2, 'USD')`, walletID, balance)
	if err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
}

func TestPostgres_TransferHappyPath(t *testing.T) {
	db, repos := setup(t)
	seedWallet(t, db.Pool(), "w_src", 1_000)
	seedWallet(t, db.Pool(), "w_dst", 0)

	svc := service.NewTransferService(repos, db, id.NewGenerator("txn", 8), nil)

	res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "pg-happy",
		FromWalletID:   "w_src",
		ToWalletID:     "w_dst",
		Amount:         250,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if res.Transfer.State != domain.TransferProcessed {
		t.Fatalf("state = %s, want PROCESSED", res.Transfer.State)
	}

	src, _ := repos.Wallets.Get(context.Background(), "w_src")
	if src.Balance.Amount != 750 {
		t.Fatalf("source balance = %d, want 750", src.Balance.Amount)
	}
	srcSum, _ := repos.Ledger.SumByWallet(context.Background(), "w_src")
	dstSum, _ := repos.Ledger.SumByWallet(context.Background(), "w_dst")
	if srcSum.Amount != -250 || dstSum.Amount != 250 {
		t.Fatalf("ledger sums = %d / %d, want -250 / 250", srcSum.Amount, dstSum.Amount)
	}
}

func TestPostgres_DuplicateIdempotencyKeyRejectedByConstraint(t *testing.T) {
	db, repos := setup(t)
	ctx := context.Background()
	seedWallet(t, db.Pool(), "w_src", 1_000)
	seedWallet(t, db.Pool(), "w_dst", 0)

	first, err := domain.NewTransfer("txn_a", "dup", "w_src", "w_dst", domain.Money{Amount: 1, Currency: "USD"})
	if err != nil {
		t.Fatalf("build transfer: %v", err)
	}
	if err := repos.Transfers.Create(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}

	second, _ := domain.NewTransfer("txn_b", "dup", "w_src", "w_dst", domain.Money{Amount: 2, Currency: "USD"})
	if err := repos.Transfers.Create(ctx, second); err != domain.ErrDuplicateIdempotencyKey {
		t.Fatalf("second create err = %v, want ErrDuplicateIdempotencyKey", err)
	}
}

// TestPostgres_ConcurrentDebits proves SELECT ... FOR UPDATE serialises debits
// of one wallet across real connections: no double spend, ledger balances.
func TestPostgres_ConcurrentDebits(t *testing.T) {
	db, repos := setup(t)
	seedWallet(t, db.Pool(), "w_src", 1_000)
	seedWallet(t, db.Pool(), "w_dst", 0)

	svc := service.NewTransferService(repos, db, id.NewGenerator("txn", 10), nil)

	const workers = 50
	const amount = 100

	var processed, failed atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
				IdempotencyKey: id.NewGenerator("k", 8).NewID(),
				FromWalletID:   "w_src",
				ToWalletID:     "w_dst",
				Amount:         amount,
			})
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			switch res.Transfer.State {
			case domain.TransferProcessed:
				processed.Add(1)
			case domain.TransferFailed:
				failed.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if processed.Load() != 10 {
		t.Fatalf("processed = %d, want 10", processed.Load())
	}
	src, _ := repos.Wallets.Get(context.Background(), "w_src")
	dst, _ := repos.Wallets.Get(context.Background(), "w_dst")
	if src.Balance.Amount != 0 || dst.Balance.Amount != 1_000 {
		t.Fatalf("balances = %d / %d, want 0 / 1000", src.Balance.Amount, dst.Balance.Amount)
	}

	var ledgerCount int
	_ = db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries`).Scan(&ledgerCount)
	if ledgerCount != 20 {
		t.Fatalf("ledger rows = %d, want 20", ledgerCount)
	}
}
