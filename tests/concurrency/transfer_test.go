package concurrency

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"wallet-transfer/internal/transfer/domain"
	transferrepository "wallet-transfer/internal/transfer/repository"
)

func TestConcurrentSameIdempotencyKey(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run against PostgreSQL")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	from, to := uuid.NewString(), uuid.NewString()
	if _, err = db.ExecContext(ctx, `INSERT INTO wallets (id,name,balance,currency) VALUES ($1,'concurrent-source',1000,'USD'),($2,'concurrent-destination',0,'USD')`, from, to); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM wallets WHERE id IN ($1,$2)`, from, to)
	repository := transferrepository.New(db)
	request := domain.CreateRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 10}
	responses := make([]domain.Transfer, 20)
	errorsFound := make([]error, 20)
	var group sync.WaitGroup
	for i := range responses {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			responses[index], errorsFound[index] = repository.Create(ctx, request)
		}(i)
	}
	group.Wait()
	for index, err := range errorsFound {
		if err != nil {
			t.Fatalf("request %d failed: %v", index, err)
		}
	}
	if responses[0].ID == "" {
		t.Fatal("no transfer ID returned")
	}
	for _, response := range responses[1:] {
		if response.ID != responses[0].ID {
			t.Fatalf("different transfer IDs: %s and %s", responses[0].ID, response.ID)
		}
	}
	var transfers, entries int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transfers WHERE idempotency_key=$1`, request.IdempotencyKey).Scan(&transfers); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id=$1`, responses[0].ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if transfers != 1 || entries != 2 {
		t.Fatalf("expected one transfer and two ledger entries, got %d and %d", transfers, entries)
	}
}
