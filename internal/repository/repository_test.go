package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aks-95/wallet-transfer-assignment/internal/domain"
	"github.com/aks-95/wallet-transfer-assignment/internal/migrate"
	"github.com/aks-95/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *repository.DB {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:root@localhost:5432/wallet_transfer?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	db, err := repository.New(ctx, url)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}

	ctx = context.Background()
	require.NoError(t, migrate.Up(ctx, db.Pool))
	require.NoError(t, resetTables(ctx, db))

	t.Cleanup(db.Close)
	return db
}

func resetTables(ctx context.Context, db *repository.DB) error {
	_, err := db.Pool.Exec(ctx, `TRUNCATE TABLE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE`)
	return err
}

func TestCreateAndGetWallet(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	created, err := db.CreateWallet(ctx, 1500)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, int64(1500), created.Balance)

	fetched, err := db.GetWallet(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, created.Balance, fetched.Balance)
}

func TestGetWalletNotFound(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, err := db.GetWallet(ctx, uuid.New())
	require.ErrorIs(t, err, repository.ErrNotFound)
}

func TestTransferRepository(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	var created *domain.Transfer
	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		var txErr error
		created, txErr = repository.InsertTransfer(ctx, tx, repository.InsertTransferParams{
			IdempotencyKey: "transfer-key-1",
			FromWalletID:   from.ID,
			ToWalletID:     to.ID,
			Amount:         250,
			Status:         domain.TransferStatusPending,
		})
		return txErr
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, domain.TransferStatusPending, created.Status)

	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		fetched, txErr := repository.GetTransferByIdempotencyKey(ctx, tx, "transfer-key-1")
		require.NoError(t, txErr)
		assert.Equal(t, created.ID, fetched.ID)
		return nil
	})
	require.NoError(t, err)
}

func TestUpdateTransferStatus(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 500)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	var transfer *domain.Transfer
	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		var txErr error
		transfer, txErr = repository.InsertTransfer(ctx, tx, repository.InsertTransferParams{
			IdempotencyKey: "status-key",
			FromWalletID:   from.ID,
			ToWalletID:     to.ID,
			Amount:         100,
			Status:         domain.TransferStatusPending,
		})
		return txErr
	})
	require.NoError(t, err)

	reason := "insufficient funds"
	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		updated, txErr := repository.UpdateTransferStatus(ctx, tx, transfer.ID, domain.TransferStatusFailed, &reason)
		require.NoError(t, txErr)
		assert.Equal(t, domain.TransferStatusFailed, updated.Status)
		require.NotNil(t, updated.FailureReason)
		assert.Equal(t, reason, *updated.FailureReason)
		return nil
	})
	require.NoError(t, err)
}

func TestWalletBalanceUpdateWithLock(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		locked, txErr := repository.GetWalletForUpdate(ctx, tx, from.ID)
		require.NoError(t, txErr)
		assert.Equal(t, int64(1000), locked.Balance)

		require.NoError(t, repository.UpdateWalletBalance(ctx, tx, from.ID, -300))
		require.NoError(t, repository.UpdateWalletBalance(ctx, tx, to.ID, 300))
		return nil
	})
	require.NoError(t, err)

	updatedFrom, err := db.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	updatedTo, err := db.GetWallet(ctx, to.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(700), updatedFrom.Balance)
	assert.Equal(t, int64(300), updatedTo.Balance)
}

func TestLedgerEntries(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	var transferID uuid.UUID
	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		transfer, txErr := repository.InsertTransfer(ctx, tx, repository.InsertTransferParams{
			IdempotencyKey: "ledger-key",
			FromWalletID:   from.ID,
			ToWalletID:     to.ID,
			Amount:         125,
			Status:         domain.TransferStatusPending,
		})
		require.NoError(t, txErr)
		transferID = transfer.ID

		_, txErr = repository.InsertLedgerEntry(ctx, tx, repository.InsertLedgerEntryParams{
			TransferID: transfer.ID,
			WalletID:   from.ID,
			Type:       domain.LedgerEntryTypeDebit,
			Amount:     125,
		})
		require.NoError(t, txErr)

		_, txErr = repository.InsertLedgerEntry(ctx, tx, repository.InsertLedgerEntryParams{
			TransferID: transfer.ID,
			WalletID:   to.ID,
			Type:       domain.LedgerEntryTypeCredit,
			Amount:     125,
		})
		return txErr
	})
	require.NoError(t, err)

	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		entries, txErr := repository.ListLedgerEntriesByTransferID(ctx, tx, transferID)
		require.NoError(t, txErr)
		require.Len(t, entries, 2)

		byType := make(map[domain.LedgerEntryType]domain.LedgerEntry, 2)
		for _, entry := range entries {
			byType[entry.Type] = entry
		}

		debit, ok := byType[domain.LedgerEntryTypeDebit]
		require.True(t, ok)
		credit, ok := byType[domain.LedgerEntryTypeCredit]
		require.True(t, ok)
		assert.Equal(t, from.ID, debit.WalletID)
		assert.Equal(t, to.ID, credit.WalletID)
		assert.Equal(t, int64(125), debit.Amount)
		assert.Equal(t, int64(125), credit.Amount)
		return nil
	})
	require.NoError(t, err)
}

func TestDuplicateIdempotencyKey(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	params := repository.InsertTransferParams{
		IdempotencyKey: "dup-key",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
		Status:         domain.TransferStatusPending,
	}

	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		_, txErr := repository.InsertTransfer(ctx, tx, params)
		return txErr
	})
	require.NoError(t, err)

	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		_, txErr := repository.InsertTransfer(ctx, tx, params)
		return txErr
	})
	require.True(t, repository.IsUniqueViolation(err))
}
