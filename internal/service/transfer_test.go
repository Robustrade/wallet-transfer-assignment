package service

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"

	_ "github.com/lib/pq"

	"wallet-transfer-assignment/internal/model"
	"wallet-transfer-assignment/internal/repository"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open(
		"postgres",
		"postgres://localhost/wallet_test?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		TRUNCATE
			idempotency_records,
			ledger_entries,
			transfers,
			wallets
		CASCADE
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO wallets (id, balance)
		VALUES ('wallet_1', 1000), ('wallet_2', 500)
	`)
	if err != nil {
		t.Fatal(err)
	}

	return db
}

func TestTransfer(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := repository.New(db)
	svc := NewTransferService(db, repo)

	transfer, err := svc.Transfer(model.TransferRequest{
		IdempotencyKey: "test-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})

	if err != nil {
		t.Fatal(err)
	}

	if transfer.Status != model.Processed {
		t.Fatalf("expected PROCESSED, got %s", transfer.Status)
	}

	var balance1, balance2 int64

	err = db.QueryRow(
		"SELECT balance FROM wallets WHERE id = 'wallet_1'",
	).Scan(&balance1)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRow(
		"SELECT balance FROM wallets WHERE id = 'wallet_2'",
	).Scan(&balance2)
	if err != nil {
		t.Fatal(err)
	}

	if balance1 != 900 {
		t.Fatalf("expected wallet_1 balance 900, got %d", balance1)
	}

	if balance2 != 600 {
		t.Fatalf("expected wallet_2 balance 600, got %d", balance2)
	}
}

func TestTransferIdempotency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := repository.New(db)
	svc := NewTransferService(db, repo)

	req := model.TransferRequest{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := svc.Transfer(req)
	if err != nil {
		t.Fatal(err)
	}

	second, err := svc.Transfer(req)
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected same transfer ID, got %s and %s", first.ID, second.ID)
	}

	var count int

	err = db.QueryRow(
		"SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1",
		first.ID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", count)
	}
}

func TestTransferInsufficientFunds(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := repository.New(db)
	svc := NewTransferService(db, repo)

	_, err := svc.Transfer(model.TransferRequest{
		IdempotencyKey: "insufficient-funds",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         2000,
	})

	if err != ErrInsufficientFunds {
		t.Fatalf("expected insufficient funds error, got %v", err)
	}
}

func TestConcurrentTransfers(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := repository.New(db)
	svc := NewTransferService(db, repo)

	const transfers = 10
	var wg sync.WaitGroup

	successes := 0
	var mu sync.Mutex

	wg.Add(transfers)

	for i := 0; i < transfers; i++ {
		go func(i int) {
			defer wg.Done()

			_, err := svc.Transfer(model.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         100,
			})

			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if successes != 10 {
		t.Fatalf("expected 10 successful transfers, got %d", successes)
	}

	var balance int64

	err := db.QueryRow(
		"SELECT balance FROM wallets WHERE id = 'wallet_1'",
	).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}

	if balance != 0 {
		t.Fatalf("expected wallet_1 balance 0, got %d", balance)
	}

	var ledgerCount int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE wallet_id = 'wallet_1'
		  AND entry_type = 'DEBIT'
	`).Scan(&ledgerCount)
	if err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 10 {
		t.Fatalf("expected 10 debit entries, got %d", ledgerCount)
	}
}
