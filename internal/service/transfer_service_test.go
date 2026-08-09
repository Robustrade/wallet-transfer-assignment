package service_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/shanks/wallet-transfer-assignment/internal/repository"
	"github.com/shanks/wallet-transfer-assignment/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable"
	}

	db, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, db.PingContext(ctx))

	schemaDDL := `
		CREATE TABLE IF NOT EXISTS wallets (
			id VARCHAR(64) PRIMARY KEY,
			balance BIGINT NOT NULL CHECK (balance >= 0),
			currency VARCHAR(3) NOT NULL DEFAULT 'USD',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS transfers (
			id VARCHAR(64) PRIMARY KEY,
			idempotency_key VARCHAR(255) UNIQUE NOT NULL,
			from_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
			to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
			amount BIGINT NOT NULL CHECK (amount > 0),
			status VARCHAR(32) NOT NULL,
			failure_reason TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS ledger_entries (
			id VARCHAR(64) PRIMARY KEY,
			transfer_id VARCHAR(64) NOT NULL REFERENCES transfers(id),
			wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
			entry_type VARCHAR(16) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
			amount BIGINT NOT NULL CHECK (amount > 0),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT uniq_ledger_transfer_type UNIQUE (transfer_id, entry_type)
		);

		CREATE TABLE IF NOT EXISTS idempotency_records (
			idempotency_key VARCHAR(255) PRIMARY KEY,
			request_hash VARCHAR(64) NOT NULL,
			status VARCHAR(32) NOT NULL,
			response_code INT,
			response_body TEXT,
			transfer_id VARCHAR(64),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets CASCADE;
	`

	_, err = db.ExecContext(ctx, schemaDDL)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func setupTestServices(db *sql.DB) (*service.WalletService, *service.TransferService) {
	txMgr := repository.NewPostgresTxManager(db)
	walletRepo := repository.NewPostgresWalletRepository(db)
	transferRepo := repository.NewPostgresTransferRepository(db)
	ledgerRepo := repository.NewPostgresLedgerRepository(db)
	idempotencyRepo := repository.NewPostgresIdempotencyRepository(db)

	walletSvc := service.NewWalletService(txMgr, walletRepo, transferRepo, ledgerRepo)
	transferSvc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	return walletSvc, transferSvc
}

func TestTransferService_SuccessfulTransfer(t *testing.T) {
	db := setupTestDB(t)
	walletSvc, transferSvc := setupTestServices(db)
	ctx := context.Background()

	// Seed wallets
	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_src", InitialBalance: 1000})
	require.NoError(t, err)
	w2, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_dst", InitialBalance: 500})
	require.NoError(t, err)

	res, err := transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "idem_tx1",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         300,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.IsReplayed)
	assert.Equal(t, domain.TransferStatusProcessed, res.Transfer.Status)
	assert.Equal(t, int64(300), res.Transfer.Amount)
	assert.Len(t, res.Ledger, 2)

	// Check double-entry ledger rule: 1 DEBIT for w1, 1 CREDIT for w2
	var debitEntry, creditEntry domain.LedgerEntry
	for _, entry := range res.Ledger {
		if entry.Type == domain.LedgerEntryTypeDebit {
			debitEntry = entry
		} else if entry.Type == domain.LedgerEntryTypeCredit {
			creditEntry = entry
		}
	}

	assert.Equal(t, w1.ID, debitEntry.WalletID)
	assert.Equal(t, int64(300), debitEntry.Amount)
	assert.Equal(t, w2.ID, creditEntry.WalletID)
	assert.Equal(t, int64(300), creditEntry.Amount)

	// Verify updated wallet balances
	updatedW1, err := walletSvc.GetWallet(ctx, w1.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(700), updatedW1.Balance)

	updatedW2, err := walletSvc.GetWallet(ctx, w2.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(800), updatedW2.Balance)
}

func TestTransferService_IdempotencyReplay(t *testing.T) {
	db := setupTestDB(t)
	walletSvc, transferSvc := setupTestServices(db)
	ctx := context.Background()

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_idem1", InitialBalance: 1000})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_idem2", InitialBalance: 500})

	input := service.CreateTransferInput{
		IdempotencyKey: "key_replay_123",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         200,
	}

	// First execution
	res1, err1 := transferSvc.CreateTransfer(ctx, input)
	require.NoError(t, err1)
	assert.False(t, res1.IsReplayed)

	// Second execution (duplicate request)
	res2, err2 := transferSvc.CreateTransfer(ctx, input)
	require.NoError(t, err2)
	assert.True(t, res2.IsReplayed)
	assert.Equal(t, res1.Transfer.ID, res2.Transfer.ID)

	// Balances should reflect only ONE transfer
	w1Updated, _ := walletSvc.GetWallet(ctx, w1.ID)
	assert.Equal(t, int64(800), w1Updated.Balance)
	w2Updated, _ := walletSvc.GetWallet(ctx, w2.ID)
	assert.Equal(t, int64(700), w2Updated.Balance)
}

func TestTransferService_IdempotencyMismatchKeyReused(t *testing.T) {
	db := setupTestDB(t)
	walletSvc, transferSvc := setupTestServices(db)
	ctx := context.Background()

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_m1", InitialBalance: 1000})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_m2", InitialBalance: 500})

	input1 := service.CreateTransferInput{
		IdempotencyKey: "key_mismatch",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         200,
	}
	_, err := transferSvc.CreateTransfer(ctx, input1)
	require.NoError(t, err)

	// Reuse same key with different amount
	input2 := service.CreateTransferInput{
		IdempotencyKey: "key_mismatch",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         300,
	}
	_, err2 := transferSvc.CreateTransfer(ctx, input2)
	assert.ErrorIs(t, err2, domain.ErrIdempotencyKeyReusedMismatch)
}

func TestTransferService_InsufficientFunds(t *testing.T) {
	db := setupTestDB(t)
	walletSvc, transferSvc := setupTestServices(db)
	ctx := context.Background()

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_low", InitialBalance: 50})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "w_rich", InitialBalance: 500})

	res, err := transferSvc.CreateTransfer(ctx, service.CreateTransferInput{
		IdempotencyKey: "key_insufficient",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	})

	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	require.NotNil(t, res)
	assert.Equal(t, domain.TransferStatusFailed, res.Transfer.Status)

	// Wallet balances remain unchanged
	w1Updated, _ := walletSvc.GetWallet(ctx, w1.ID)
	assert.Equal(t, int64(50), w1Updated.Balance)
}

func TestTransferService_InvalidValidationCases(t *testing.T) {
	db := setupTestDB(t)
	_, transferSvc := setupTestServices(db)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   service.CreateTransferInput
		wantErr error
	}{
		{
			name: "missing idempotency key",
			input: service.CreateTransferInput{
				IdempotencyKey: "",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         10,
			},
			wantErr: nil, // returns generic error
		},
		{
			name: "same wallet transfer",
			input: service.CreateTransferInput{
				IdempotencyKey: "key_same",
				FromWalletID:   "w1",
				ToWalletID:     "w1",
				Amount:         10,
			},
			wantErr: domain.ErrSameWalletTransfer,
		},
		{
			name: "negative amount",
			input: service.CreateTransferInput{
				IdempotencyKey: "key_neg",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         -50,
			},
			wantErr: domain.ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := transferSvc.CreateTransfer(ctx, tt.input)
			assert.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}
