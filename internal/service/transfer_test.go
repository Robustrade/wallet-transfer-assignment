package service_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aks-95/wallet-transfer-assignment/internal/domain"
	"github.com/aks-95/wallet-transfer-assignment/internal/migrate"
	"github.com/aks-95/wallet-transfer-assignment/internal/repository"
	"github.com/aks-95/wallet-transfer-assignment/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testService(t *testing.T) (*service.TransferService, *repository.DB) {
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
	_, err = db.Pool.Exec(ctx, `TRUNCATE TABLE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	t.Cleanup(db.Close)
	return service.NewTransferService(db), db
}

func TestCreateTransferSuccess(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "success-key",
		FromWalletID:   from.ID.String(),
		ToWalletID:     to.ID.String(),
		Amount:         250,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Replayed)
	assert.Equal(t, domain.TransferStatusProcessed, result.Transfer.Status)

	updatedFrom, err := db.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	updatedTo, err := db.GetWallet(ctx, to.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(750), updatedFrom.Balance)
	assert.Equal(t, int64(250), updatedTo.Balance)

	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		entries, txErr := repository.ListLedgerEntriesByTransferID(ctx, tx, result.Transfer.ID)
		require.NoError(t, txErr)
		require.Len(t, entries, 2)
		return nil
	})
	require.NoError(t, err)
}

func TestCreateTransferIdempotentReplay(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "replay-key",
		FromWalletID:   from.ID.String(),
		ToWalletID:     to.ID.String(),
		Amount:         100,
	}

	first, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)

	second, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	assert.True(t, second.Replayed)
	assert.Equal(t, first.Transfer.ID, second.Transfer.ID)

	updatedFrom, err := db.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(900), updatedFrom.Balance)
}

func TestCreateTransferIdempotencyConflict(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	_, err = svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "conflict-key",
		FromWalletID:   from.ID.String(),
		ToWalletID:     to.ID.String(),
		Amount:         100,
	})
	require.NoError(t, err)

	_, err = svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "conflict-key",
		FromWalletID:   from.ID.String(),
		ToWalletID:     to.ID.String(),
		Amount:         200,
	})
	require.ErrorIs(t, err, service.ErrIdempotencyConflict)
}

func TestCreateTransferInsufficientFunds(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 50)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "insufficient-key",
		FromWalletID:   from.ID.String(),
		ToWalletID:     to.ID.String(),
		Amount:         100,
	})
	require.ErrorIs(t, err, service.ErrInsufficientFunds)
	require.NotNil(t, result)
	assert.Equal(t, domain.TransferStatusFailed, result.Transfer.Status)

	updatedFrom, err := db.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	updatedTo, err := db.GetWallet(ctx, to.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(50), updatedFrom.Balance)
	assert.Equal(t, int64(0), updatedTo.Balance)

	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		entries, txErr := repository.ListLedgerEntriesByTransferID(ctx, tx, result.Transfer.ID)
		require.NoError(t, txErr)
		assert.Empty(t, entries)
		return nil
	})
	require.NoError(t, err)
}

func TestCreateTransferWalletNotFound(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)

	result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "missing-wallet-key",
		FromWalletID:   from.ID.String(),
		ToWalletID:     uuid.New().String(),
		Amount:         100,
	})
	require.ErrorIs(t, err, service.ErrWalletNotFound)
	assert.Nil(t, result)
}

func TestCreateTransferConcurrentDebits(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 100)
	require.NoError(t, err)
	toA, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)
	toB, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	failures := 0

	run := func(key string, toID uuid.UUID) {
		defer wg.Done()
		result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
			IdempotencyKey: key,
			FromWalletID:   from.ID.String(),
			ToWalletID:     toID.String(),
			Amount:         100,
		})

		mu.Lock()
		defer mu.Unlock()

		if err != nil {
			require.ErrorIs(t, err, service.ErrInsufficientFunds)
			require.NotNil(t, result)
			assert.Equal(t, domain.TransferStatusFailed, result.Transfer.Status)
			failures++
			return
		}

		require.NotNil(t, result)
		assert.Equal(t, domain.TransferStatusProcessed, result.Transfer.Status)
		successes++
	}

	wg.Add(2)
	go run("concurrent-a", toA.ID)
	go run("concurrent-b", toB.ID)
	wg.Wait()

	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, failures)

	updatedFrom, err := db.GetWallet(ctx, from.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updatedFrom.Balance)
}
