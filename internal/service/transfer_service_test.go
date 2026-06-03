package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"wallet-transfer/internal/config"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	cfg := config.Load()
	// Use a dedicated test database
	testDBName := cfg.DBName + "_test"

	err := postgres.EnsureDatabaseExists(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, testDBName, cfg.DBSSLMode)
	require.NoError(t, err)

	testConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, testDBName, cfg.DBSSLMode)

	db, err := postgres.NewDB(testConnStr)
	require.NoError(t, err)

	err = postgres.RunMigrations(db)
	require.NoError(t, err)

	return db
}

func truncateTables(t *testing.T, db *gorm.DB) {
	err := db.Exec("TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets CASCADE").Error
	require.NoError(t, err)
}

func TestProcessTransfer_Success(t *testing.T) {
	db := setupTestDB(t)
	truncateTables(t, db)

	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	svc := service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	ctx := context.Background()

	// Seed wallets
	err := walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_A", Balance: 500})
	require.NoError(t, err)
	err = walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_B", Balance: 100})
	require.NoError(t, err)

	req := &service.ProcessTransferRequest{
		IdempotencyKey: "key_1",
		FromWalletID:   "wallet_A",
		ToWalletID:     "wallet_B",
		Amount:         200,
	}

	resp, code, err := svc.ProcessTransfer(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.NotEmpty(t, resp.TransferID)
	assert.Equal(t, domain.TransferStatusProcessed, resp.Status)

	// Verify balances
	wA, err := walletRepo.Get(ctx, db, "wallet_A")
	require.NoError(t, err)
	assert.Equal(t, int64(300), wA.Balance)

	wB, err := walletRepo.Get(ctx, db, "wallet_B")
	require.NoError(t, err)
	assert.Equal(t, int64(300), wB.Balance)

	// Verify ledger entries
	entries, err := ledgerRepo.GetByTransferID(ctx, db, resp.TransferID)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	var debit, credit domain.LedgerEntry
	for _, entry := range entries {
		if entry.Type == domain.EntryTypeDebit {
			debit = entry
		} else {
			credit = entry
		}
	}

	assert.Equal(t, "wallet_A", debit.WalletID)
	assert.Equal(t, int64(200), debit.Amount)
	assert.Equal(t, "wallet_B", credit.WalletID)
	assert.Equal(t, int64(200), credit.Amount)
}

func TestProcessTransfer_InsufficientFunds(t *testing.T) {
	db := setupTestDB(t)
	truncateTables(t, db)

	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	svc := service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	ctx := context.Background()

	err := walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_A", Balance: 50})
	require.NoError(t, err)
	err = walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_B", Balance: 100})
	require.NoError(t, err)

	req := &service.ProcessTransferRequest{
		IdempotencyKey: "key_insufficient",
		FromWalletID:   "wallet_A",
		ToWalletID:     "wallet_B",
		Amount:         200,
	}

	resp, code, err := svc.ProcessTransfer(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 422, code)
	assert.Equal(t, domain.TransferStatusFailed, resp.Status)
	assert.Equal(t, domain.ErrInsufficientFunds.Error(), resp.Error)

	// Balances should remain unchanged
	wA, _ := walletRepo.Get(ctx, db, "wallet_A")
	assert.Equal(t, int64(50), wA.Balance)
	wB, _ := walletRepo.Get(ctx, db, "wallet_B")
	assert.Equal(t, int64(100), wB.Balance)

	// No ledger entries should exist
	entries, _ := ledgerRepo.GetByTransferID(ctx, db, resp.TransferID)
	assert.Empty(t, entries)
}

func TestProcessTransfer_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	truncateTables(t, db)

	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	svc := service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	ctx := context.Background()

	err := walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_A", Balance: 1000})
	require.NoError(t, err)
	err = walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_B", Balance: 100})
	require.NoError(t, err)

	req := &service.ProcessTransferRequest{
		IdempotencyKey: "idem_key_1",
		FromWalletID:   "wallet_A",
		ToWalletID:     "wallet_B",
		Amount:         200,
	}

	// First request
	resp1, code1, err := svc.ProcessTransfer(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 201, code1)

	// Duplicate request
	resp2, code2, err := svc.ProcessTransfer(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 201, code2)
	assert.Equal(t, resp1.TransferID, resp2.TransferID)
	assert.Equal(t, resp1.Status, resp2.Status)

	// Balance should only have been deducted ONCE
	wA, _ := walletRepo.Get(ctx, db, "wallet_A")
	assert.Equal(t, int64(800), wA.Balance)
}

func TestProcessTransfer_Concurrency(t *testing.T) {
	db := setupTestDB(t)
	truncateTables(t, db)

	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	svc := service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	ctx := context.Background()

	// Seed wallets with 1000 each
	err := walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_X", Balance: 1000})
	require.NoError(t, err)
	err = walletRepo.Create(ctx, db, &domain.Wallet{ID: "wallet_Y", Balance: 1000})
	require.NoError(t, err)

	var wg sync.WaitGroup
	numTransfers := 10

	// 10 concurrent transfers of 50 from X to Y
	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &service.ProcessTransferRequest{
				IdempotencyKey: fmt.Sprintf("x_to_y_%d", idx),
				FromWalletID:   "wallet_X",
				ToWalletID:     "wallet_Y",
				Amount:         50,
			}
			_, _, _ = svc.ProcessTransfer(ctx, req)
		}(i)
	}

	// 10 concurrent transfers of 50 from Y to X
	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &service.ProcessTransferRequest{
				IdempotencyKey: fmt.Sprintf("y_to_x_%d", idx),
				FromWalletID:   "wallet_Y",
				ToWalletID:     "wallet_X",
				Amount:         50,
			}
			_, _, _ = svc.ProcessTransfer(ctx, req)
		}(i)
	}

	wg.Wait()

	// Final balances should still be 1000 each, as they cancel each other out
	wX, err := walletRepo.Get(ctx, db, "wallet_X")
	require.NoError(t, err)
	wY, err := walletRepo.Get(ctx, db, "wallet_Y")
	require.NoError(t, err)

	assert.Equal(t, int64(1000), wX.Balance, "wallet_X balance should be 1000")
	assert.Equal(t, int64(1000), wY.Balance, "wallet_Y balance should be 1000")
}
