package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository/postgres"

	_ "github.com/lib/pq"
)

func setupDB(t *testing.T) *sql.DB {
	// Look for a postgres connection string in the environment
	dbURL := os.Getenv("TEST_DB_URL")
	if dbURL == "" {
		// Skip the test gracefully if no database is available, preventing CI/reviewer failures
		t.Skip("Skipping Postgres integration tests because TEST_DB_URL is not set")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to open postgres db: %v", err)
	}

	// Schema setup (Dropping and recreating to ensure a clean slate)
	_, err = db.Exec(`
		DROP TABLE IF EXISTS ledger_entries CASCADE;
		DROP TABLE IF EXISTS transfers CASCADE;
		DROP TABLE IF EXISTS wallets CASCADE;

		CREATE TABLE wallets (
			id VARCHAR(255) PRIMARY KEY,
			balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);
		CREATE TABLE transfers (
			id VARCHAR(255) PRIMARY KEY,
			idempotency_key VARCHAR(255) UNIQUE NOT NULL,
			from_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
			to_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
			amount BIGINT NOT NULL,
			state VARCHAR(50) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);
		CREATE TABLE ledger_entries (
			id VARCHAR(255) PRIMARY KEY,
			wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
			transfer_id VARCHAR(255) NOT NULL REFERENCES transfers(id),
			type VARCHAR(50) NOT NULL,
			amount BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);
	`)
	if err != nil {
		t.Fatalf("failed to setup schema: %v", err)
	}

	return db
}

func TestPostgresRepository_Concurrency(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := postgres.NewPostgresRepository(db)
	ctx := context.Background()

	// Initialize wallets
	_, err := db.Exec("INSERT INTO wallets (id, balance) VALUES ('pg-wallet-1', 100), ('pg-wallet-2', 0)")
	if err != nil {
		t.Fatalf("failed to insert initial wallets: %v", err)
	}

	var wg sync.WaitGroup
	successes := 0
	var mu sync.Mutex

	// Fire 3 concurrent requests of 50 each
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &domain.Transfer{
				IdempotencyKey: "pg-concurrent-key-" + string(rune(idx+'a')),
				FromWalletID:   "pg-wallet-1",
				ToWalletID:     "pg-wallet-2",
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
	db.QueryRow("SELECT balance FROM wallets WHERE id = 'pg-wallet-1'").Scan(&b1)
	if b1 < 0 {
		t.Errorf("expected final balance to be >= 0, got %d", b1)
	}
}

func TestPostgresRepository_Idempotency(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := postgres.NewPostgresRepository(db)
	ctx := context.Background()

	_, err := db.Exec("INSERT INTO wallets (id, balance) VALUES ('pg-wallet-3', 100), ('pg-wallet-4', 0)")
	if err != nil {
		t.Fatalf("failed to insert initial wallets: %v", err)
	}

	req := &domain.Transfer{
		IdempotencyKey: "pg-duplicate-key",
		FromWalletID:   "pg-wallet-3",
		ToWalletID:     "pg-wallet-4",
		Amount:         10,
	}

	// First transfer should succeed
	_, err = repo.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("expected first transfer to succeed, got %v", err)
	}

	// Second transfer with same key should fail with ErrDuplicateTransfer
	req2 := &domain.Transfer{
		IdempotencyKey: "pg-duplicate-key",
		FromWalletID:   "pg-wallet-3",
		ToWalletID:     "pg-wallet-4",
		Amount:         20,
	}
	_, err = repo.ExecuteTransfer(ctx, req2)
	if err != domain.ErrDuplicateTransfer {
		t.Fatalf("expected ErrDuplicateTransfer, got %v", err)
	}
}

func TestPostgresRepository_GetTransferByIdempotencyKey(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := postgres.NewPostgresRepository(db)
	ctx := context.Background()

	_, err := db.Exec("INSERT INTO wallets (id, balance) VALUES ('pg-wallet-5', 100), ('pg-wallet-6', 0)")
	if err != nil {
		t.Fatalf("failed to insert initial wallets: %v", err)
	}

	req := &domain.Transfer{
		IdempotencyKey: "test-get-key",
		FromWalletID:   "pg-wallet-5",
		ToWalletID:     "pg-wallet-6",
		Amount:         50,
	}

	// Create a transfer
	createdTransfer, err := repo.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("failed to execute transfer: %v", err)
	}

	// Test getting an existing transfer
	retrievedTransfer, err := repo.GetTransferByIdempotencyKey(ctx, "test-get-key")
	if err != nil {
		t.Fatalf("failed to get transfer: %v", err)
	}
	if retrievedTransfer == nil || retrievedTransfer.ID != createdTransfer.ID {
		t.Errorf("retrieved transfer doesn't match created transfer")
	}

	// Test getting a non-existent transfer
	missingTransfer, err := repo.GetTransferByIdempotencyKey(ctx, "non-existent-key")
	if err != nil {
		t.Fatalf("expected nil error for missing transfer, got %v", err)
	}
	if missingTransfer != nil {
		t.Errorf("expected nil transfer for missing key, got %v", missingTransfer)
	}
}

func TestPostgresRepository_WalletNotFound(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := postgres.NewPostgresRepository(db)
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

	req2 := &domain.Transfer{
		IdempotencyKey: "test-not-found-key-2",
		FromWalletID:   "pg-wallet-7", // This wallet exists from the next test
		ToWalletID:     "missing-wallet-3",
		Amount:         10,
	}
	db.Exec("INSERT INTO wallets (id, balance) VALUES ('pg-wallet-7', 50)") // ensure it exists
	_, err = repo.ExecuteTransfer(ctx, req2)
	if err != domain.ErrWalletNotFound {
		t.Fatalf("expected ErrWalletNotFound for second wallet, got %v", err)
	}
}

func TestPostgresRepository_ContextCancellation(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := postgres.NewPostgresRepository(db)

	// Create a context and cancel it immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &domain.Transfer{
		IdempotencyKey: "test-cancel-key",
		FromWalletID:   "pg-wallet-1",
		ToWalletID:     "pg-wallet-2",
		Amount:         10,
	}

	// This should fail immediately at BeginTx or QueryRowContext due to context canceled
	_, err := repo.ExecuteTransfer(ctx, req)
	if err == nil {
		t.Fatalf("expected error due to canceled context, got nil")
	}

	// Also test the Get method
	_, err = repo.GetTransferByIdempotencyKey(ctx, "test-cancel-key")
	if err == nil {
		t.Fatalf("expected error due to canceled context on Get, got nil")
	}
}

func TestPostgresRepository_InsufficientFunds(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	repo := postgres.NewPostgresRepository(db)
	ctx := context.Background()

	_, err := db.Exec("INSERT INTO wallets (id, balance) VALUES ('pg-wallet-7', 50), ('pg-wallet-8', 0)")
	if err != nil {
		t.Fatalf("failed to insert initial wallets: %v", err)
	}

	req := &domain.Transfer{
		IdempotencyKey: "test-insufficient-key",
		FromWalletID:   "pg-wallet-7",
		ToWalletID:     "pg-wallet-8",
		Amount:         100, // Balance is only 50
	}

	_, err = repo.ExecuteTransfer(ctx, req)
	if err != domain.ErrInsufficientFunds {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
}
