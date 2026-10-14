package service_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"wallet-transfer/repository"
	"wallet-transfer/service"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	query := `
	CREATE TABLE wallets (id TEXT PRIMARY KEY, balance INTEGER NOT NULL);
	CREATE TABLE transfers (id TEXT PRIMARY KEY, idempotency_key TEXT UNIQUE NOT NULL, from_wallet_id TEXT NOT NULL, to_wallet_id TEXT NOT NULL, amount INTEGER NOT NULL, status TEXT NOT NULL);
	CREATE TABLE ledger_entries (id TEXT PRIMARY KEY, transfer_id TEXT NOT NULL, wallet_id TEXT NOT NULL, type TEXT NOT NULL, amount INTEGER NOT NULL);
	`
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("failed to setup schema: %v", err)
	}

	return db
}

func TestTransferService(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := repository.NewWalletRepository(db)
	svc := service.NewTransferService(repo)
	ctx := context.Background()

	// Seed Wallets
	_, _ = db.Exec(`INSERT INTO wallets (id, balance) VALUES ('walletA', 100), ('walletB', 50)`)

	t.Run("executes valid transfer and updates balances", func(t *testing.T) {
		req := service.TransferRequest{
			IdempotencyKey: "tx-100",
			FromWalletID:   "walletA",
			ToWalletID:     "walletB",
			Amount:         30,
		}

		res, err := svc.ProcessTransfer(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if res.IsReplay {
			t.Errorf("expected IsReplay = false, got true")
		}

		// Check Balances
		var balA, balB int64
		_ = db.QueryRow(`SELECT balance FROM wallets WHERE id = 'walletA'`).Scan(&balA)
		_ = db.QueryRow(`SELECT balance FROM wallets WHERE id = 'walletB'`).Scan(&balB)

		if balA != 70 || balB != 80 {
			t.Errorf("balances incorrect: A=%d (expected 70), B=%d (expected 80)", balA, balB)
		}
	})

	t.Run("prevents double spending on duplicate idempotency key", func(t *testing.T) {
		req := service.TransferRequest{
			IdempotencyKey: "tx-100", // Same key as above
			FromWalletID:   "walletA",
			ToWalletID:     "walletB",
			Amount:         30,
		}

		res, err := svc.ProcessTransfer(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !res.IsReplay {
			t.Errorf("expected IsReplay = true for duplicate key")
		}
	})
}