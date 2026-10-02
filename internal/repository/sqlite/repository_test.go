package sqlite_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository/sqlite"

	_ "github.com/mattn/go-sqlite3"
)

func setupDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	// Schema setup
	_, err = db.Exec(`
		CREATE TABLE wallets (
			id VARCHAR(255) PRIMARY KEY,
			balance BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);
		CREATE TABLE transfers (
			id VARCHAR(255) PRIMARY KEY,
			idempotency_key VARCHAR(255) UNIQUE NOT NULL,
			from_wallet_id VARCHAR(255) NOT NULL,
			to_wallet_id VARCHAR(255) NOT NULL,
			amount BIGINT NOT NULL,
			state VARCHAR(50) NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);
		CREATE TABLE ledger_entries (
			id VARCHAR(255) PRIMARY KEY,
			wallet_id VARCHAR(255) NOT NULL,
			transfer_id VARCHAR(255) NOT NULL,
			type VARCHAR(50) NOT NULL,
			amount BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("failed to setup schema: %v", err)
	}

	return db
}

func TestSQLiteRepository_ExecuteTransfer(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteRepository(db)
	ctx := context.Background()

	// Setup initial wallets
	now := time.Now()
	db.Exec("INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('w1', 100, ?, ?)", now, now)
	db.Exec("INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('w2', 0, ?, ?)", now, now)

	t.Run("Successful Transfer", func(t *testing.T) {
		req := &domain.Transfer{
			IdempotencyKey: "key-1",
			FromWalletID:   "w1",
			ToWalletID:     "w2",
			Amount:         40,
		}

		res, err := repo.ExecuteTransfer(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.State != domain.TransferStateProcessed {
			t.Errorf("expected processed state, got %v", res.State)
		}

		// Verify balances
		var b1, b2 int64
		db.QueryRow("SELECT balance FROM wallets WHERE id = 'w1'").Scan(&b1)
		db.QueryRow("SELECT balance FROM wallets WHERE id = 'w2'").Scan(&b2)
		if b1 != 60 {
			t.Errorf("expected w1 balance 60, got %d", b1)
		}
		if b2 != 40 {
			t.Errorf("expected w2 balance 40, got %d", b2)
		}

		// Verify ledger entries
		var count int
		db.QueryRow("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?", res.ID).Scan(&count)
		if count != 2 {
			t.Errorf("expected 2 ledger entries, got %d", count)
		}
	})

	t.Run("Insufficient Funds", func(t *testing.T) {
		req := &domain.Transfer{
			IdempotencyKey: "key-2",
			FromWalletID:   "w1",
			ToWalletID:     "w2",
			Amount:         100, // Balance is only 60 now
		}

		_, err := repo.ExecuteTransfer(ctx, req)
		if err != domain.ErrInsufficientFunds {
			t.Fatalf("expected ErrInsufficientFunds, got %v", err)
		}
	})

	t.Run("Duplicate Transfer Key", func(t *testing.T) {
		req := &domain.Transfer{
			IdempotencyKey: "key-1", // Same key as first successful test
			FromWalletID:   "w1",
			ToWalletID:     "w2",
			Amount:         10,
		}
		_, err := repo.ExecuteTransfer(ctx, req)
		if err != domain.ErrDuplicateTransfer {
			t.Fatalf("expected ErrDuplicateTransfer, got %v", err)
		}
	})
}

func TestSQLiteRepository_Concurrency(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteRepository(db)
	ctx := context.Background()

	now := time.Now()
	db.Exec("INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('w-concurrent-1', 100, ?, ?)", now, now)
	db.Exec("INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('w-concurrent-2', 0, ?, ?)", now, now)

	var wg sync.WaitGroup
	successes := 0
	var mu sync.Mutex

	// Fire 3 concurrent requests of 50 each
	// Only 2 should succeed, 1 should fail with insufficient funds or locked
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &domain.Transfer{
				IdempotencyKey: "concurrent-key-" + string(rune(idx)),
				FromWalletID:   "w-concurrent-1",
				ToWalletID:     "w-concurrent-2",
				Amount:         50,
			}
			_, err := repo.ExecuteTransfer(ctx, req)
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if successes > 2 {
		t.Errorf("expected max 2 successful transfers, got %d", successes)
	}

	var b1 int64
	db.QueryRow("SELECT balance FROM wallets WHERE id = 'w-concurrent-1'").Scan(&b1)
	if b1 < 0 {
		t.Errorf("expected final balance to be >= 0, got %d", b1)
	}
}

func TestSQLiteRepository_GetTransferByIdempotencyKey(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteRepository(db)
	ctx := context.Background()

	now := time.Now()
	db.Exec("INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('sqlite-wallet-1', 100, ?, ?)", now, now)
	db.Exec("INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('sqlite-wallet-2', 0, ?, ?)", now, now)

	req := &domain.Transfer{
		IdempotencyKey: "test-get-key-sqlite",
		FromWalletID:   "sqlite-wallet-1",
		ToWalletID:     "sqlite-wallet-2",
		Amount:         50,
	}

	createdTransfer, err := repo.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("failed to execute transfer: %v", err)
	}

	retrievedTransfer, err := repo.GetTransferByIdempotencyKey(ctx, "test-get-key-sqlite")
	if err != nil {
		t.Fatalf("failed to get transfer: %v", err)
	}
	if retrievedTransfer == nil || retrievedTransfer.ID != createdTransfer.ID {
		t.Errorf("retrieved transfer doesn't match created transfer")
	}

	missingTransfer, err := repo.GetTransferByIdempotencyKey(ctx, "non-existent-key")
	if err != nil {
		t.Fatalf("expected nil error for missing transfer, got %v", err)
	}
	if missingTransfer != nil {
		t.Errorf("expected nil transfer for missing key, got %v", missingTransfer)
	}
}

func TestSQLiteRepository_WalletNotFound(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteRepository(db)
	ctx := context.Background()

	req := &domain.Transfer{
		IdempotencyKey: "test-not-found-key",
		FromWalletID:   "missing-wallet-1",
		ToWalletID:     "missing-wallet-2",
		Amount:         50,
	}

	_, err := repo.ExecuteTransfer(ctx, req)
	if err != domain.ErrWalletNotFound {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}
