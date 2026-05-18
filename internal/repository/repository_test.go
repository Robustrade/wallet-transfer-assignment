package repository_test

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wallet-transfer-assignment/internal/model"
	"github.com/wallet-transfer-assignment/internal/repository"
)

func setupDB(t *testing.T) (*sql.DB, func()) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://user:password@localhost:5432/wallet?sslmode=disable"
	}
	db, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)

	_, err = db.Exec(`
		DROP TABLE IF EXISTS ledger_entries CASCADE;
		DROP TABLE IF EXISTS transfers CASCADE;
		DROP TABLE IF EXISTS idempotency_keys CASCADE;
		DROP TABLE IF EXISTS wallets CASCADE;

		CREATE TABLE wallets (
			id VARCHAR(255) PRIMARY KEY,
			balance DECIMAL(15, 2) NOT NULL DEFAULT 0.00 CHECK (balance >= 0),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE transfers (
			id VARCHAR(255) PRIMARY KEY,
			idempotency_key VARCHAR(255) UNIQUE NOT NULL,
			from_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
			to_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
			amount DECIMAL(15, 2) NOT NULL CHECK (amount > 0),
			status VARCHAR(50) NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE ledger_entries (
			id SERIAL PRIMARY KEY,
			transfer_id VARCHAR(255) NOT NULL REFERENCES transfers(id),
			wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
			type VARCHAR(50) NOT NULL,
			amount DECIMAL(15, 2) NOT NULL CHECK (amount > 0),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE idempotency_keys (
			idempotency_key VARCHAR(255) PRIMARY KEY,
			locked_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			response_code INT,
			response_body JSONB
		);

		CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
		CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
		CREATE INDEX idx_ledger_entries_transfer ON ledger_entries(transfer_id);
		CREATE INDEX idx_ledger_entries_wallet ON ledger_entries(wallet_id);
	`)
	require.NoError(t, err)

	return db, func() {
		db.Close()
	}
}

func TestConcurrentTransfers(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("Skipping integration tests. Set INTEGRATION_TEST=1 to run.")
	}

	db, cleanup := setupDB(t)
	defer cleanup()

	repo := repository.NewRepository(db)
	ctx := context.Background()

	// Create two wallets
	w1ID := "wallet_1"
	w2ID := "wallet_2"

	err := repo.CreateWallet(ctx, w1ID, 1000.0)
	require.NoError(t, err)

	err = repo.CreateWallet(ctx, w2ID, 0.0)
	require.NoError(t, err)

	var wg sync.WaitGroup
	numTransfers := 10

	errCh := make(chan error, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			transfer := &model.Transfer{
				ID:             uuid.New().String(),
				IdempotencyKey: uuid.New().String(),
				FromWalletID:   w1ID,
				ToWalletID:     w2ID,
				Amount:         10.0,
			}
			errCh <- repo.ExecuteTransfer(ctx, transfer)
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.NoError(t, err)
	}

	// Verify balances
	w1, err := repo.GetWallet(ctx, w1ID)
	require.NoError(t, err)
	assert.Equal(t, 900.0, w1.Balance)

	w2, err := repo.GetWallet(ctx, w2ID)
	require.NoError(t, err)
	assert.Equal(t, 100.0, w2.Balance)
}
