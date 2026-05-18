package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wallet-transfer-assignment/internal/model"
	"github.com/wallet-transfer-assignment/internal/repository"
	"github.com/wallet-transfer-assignment/internal/service"
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
	`)
	require.NoError(t, err)

	return db, func() {
		db.Close()
	}
}

func TestServiceBehavior(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("Skipping integration tests. Set INTEGRATION_TEST=1 to run.")
	}

	db, cleanup := setupDB(t)
	defer cleanup()

	repo := repository.NewRepository(db)
	svc := service.NewService(repo)
	ctx := context.Background()

	w1ID := "svc_wallet_1"
	w2ID := "svc_wallet_2"

	require.NoError(t, repo.CreateWallet(ctx, w1ID, 500.0))
	require.NoError(t, repo.CreateWallet(ctx, w2ID, 0.0))

	t.Run("Duplicate Request (Idempotency)", func(t *testing.T) {
		req := &model.CreateTransferRequest{
			IdempotencyKey: uuid.New().String(),
			FromWalletID:   w1ID,
			ToWalletID:     w2ID,
			Amount:         100.0,
		}

		res1 := svc.ProcessTransfer(ctx, req)
		assert.Equal(t, http.StatusOK, res1.HTTPStatus)
		var tr1 service.TransferResponse
		json.Unmarshal(res1.Response, &tr1)
		assert.NotEmpty(t, tr1.TransferID)
		assert.Equal(t, "PROCESSED", tr1.Status)

		res2 := svc.ProcessTransfer(ctx, req)
		assert.Equal(t, http.StatusOK, res2.HTTPStatus)
		var tr2 service.TransferResponse
		json.Unmarshal(res2.Response, &tr2)

		assert.Equal(t, tr1.TransferID, tr2.TransferID)
		assert.Equal(t, tr1.Status, tr2.Status)

		w1, _ := repo.GetWallet(ctx, w1ID)
		assert.Equal(t, 400.0, w1.Balance)
	})

	t.Run("Failed Transfer (Insufficient Funds)", func(t *testing.T) {
		req := &model.CreateTransferRequest{
			IdempotencyKey: uuid.New().String(),
			FromWalletID:   w1ID,
			ToWalletID:     w2ID,
			Amount:         1000.0,
		}

		res := svc.ProcessTransfer(ctx, req)
		assert.Equal(t, http.StatusBadRequest, res.HTTPStatus)
		var tr service.TransferResponse
		json.Unmarshal(res.Response, &tr)
		assert.Equal(t, "FAILED", tr.Status)
		assert.Equal(t, "Insufficient funds", tr.Message)

		w1, _ := repo.GetWallet(ctx, w1ID)
		assert.Equal(t, 400.0, w1.Balance)
	})

	t.Run("Ledger Balancing", func(t *testing.T) {
		var totalDebit, totalCredit float64
		err := db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE type = 'DEBIT'").Scan(&totalDebit)
		require.NoError(t, err)

		err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE type = 'CREDIT'").Scan(&totalCredit)
		require.NoError(t, err)

		assert.Equal(t, totalDebit, totalCredit)

		assert.Equal(t, 100.0, totalDebit)
	})
}
