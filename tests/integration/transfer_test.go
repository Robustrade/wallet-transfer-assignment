package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"wallet-transfer/internal/transfer/domain"
	transferrepository "wallet-transfer/internal/transfer/repository"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func openDatabase(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PingContext(context.Background()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createWallets(t *testing.T, db *sql.DB, sourceBalance int64) (string, string) {
	t.Helper()
	ctx := context.Background()
	from, to := uuid.NewString(), uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO wallets (id,name,balance,currency) VALUES ($1,'integration-source',$3,'USD'),($2,'integration-destination',0,'USD')`, from, to, sourceBalance)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM wallets WHERE id IN ($1,$2)`, from, to) })
	return from, to
}

func TestTransferSuccessAndIdempotency(t *testing.T) {
	db := openDatabase(t)
	from, to := createWallets(t, db, 100)
	repository := transferrepository.New(db)
	request := domain.CreateRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 40}
	first, err := repository.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("expected idempotent result, got %q and %q", first.ID, second.ID)
	}
	var sourceBalance, destinationBalance int64
	if err = db.QueryRow(`SELECT balance FROM wallets WHERE id=$1`, from).Scan(&sourceBalance); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT balance FROM wallets WHERE id=$1`, to).Scan(&destinationBalance); err != nil {
		t.Fatal(err)
	}
	if sourceBalance != 60 || destinationBalance != 40 {
		t.Fatalf("unexpected balances: %d and %d", sourceBalance, destinationBalance)
	}
	var entries int
	if err = db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id=$1`, first.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 2 {
		t.Fatalf("expected two ledger entries, got %d", entries)
	}
}

func TestTransferInsufficientFunds(t *testing.T) {
	db := openDatabase(t)
	from, to := createWallets(t, db, 50)
	repository := transferrepository.New(db)
	request := domain.CreateRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 60}
	transfer, err := repository.Create(context.Background(), request)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	if transfer.Status != domain.Failed {
		t.Fatalf("expected failed status, got %s", transfer.Status)
	}
	var entries int
	if err = db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id=$1`, transfer.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("expected no ledger entries, got %d", entries)
	}
}

func TestDifferentPayloadWithSameIdempotencyKeyIsRejected(t *testing.T) {
	db := openDatabase(t)
	from, to := createWallets(t, db, 100)
	repository := transferrepository.New(db)
	key := uuid.NewString()
	_, err := repository.Create(context.Background(), domain.CreateRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.Create(context.Background(), domain.CreateRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 20})
	if !errors.Is(err, domain.ErrIdempotencyReuse) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}
