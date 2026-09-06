package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/sqlite"
	"wallet-transfer-assignment/internal/service"
)

// newTestUnitOfWork opens an isolated in-memory SQLite database per test so
// tests don't interfere with each other.
func newTestUnitOfWork(t *testing.T) *sqlite.UnitOfWork {
	t.Helper()
	ctx := context.Background()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())
	db, err := sqlite.Open(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return sqlite.NewUnitOfWork(db)
}

func seedWallet(t *testing.T, uow *sqlite.UnitOfWork, id string, balance int64) {
	t.Helper()
	_, err := service.NewWalletService(uow).CreateWallet(context.Background(), id, balance)
	require.NoError(t, err)
}

func TestTransferService_HappyPath(t *testing.T) {
	uow := newTestUnitOfWork(t)
	seedWallet(t, uow, "wallet_1", 1000)
	seedWallet(t, uow, "wallet_2", 0)

	svc := service.NewTransferService(uow)
	result, err := svc.Execute(context.Background(), service.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	require.NoError(t, err)
	assert.False(t, result.Replayed)
	assert.Equal(t, domain.TransferProcessed, result.Transfer.Status)

	from, err := service.NewWalletService(uow).GetWallet(context.Background(), "wallet_1")
	require.NoError(t, err)
	assert.Equal(t, int64(900), from.Balance)

	to, err := service.NewWalletService(uow).GetWallet(context.Background(), "wallet_2")
	require.NoError(t, err)
	assert.Equal(t, int64(100), to.Balance)

	_, entries, err := svc.GetTransfer(context.Background(), result.Transfer.ID)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	var debit, credit *domain.LedgerEntry
	for i := range entries {
		switch entries[i].Type {
		case domain.LedgerEntryDebit:
			debit = &entries[i]
		case domain.LedgerEntryCredit:
			credit = &entries[i]
		}
	}
	require.NotNil(t, debit)
	require.NotNil(t, credit)
	assert.Equal(t, "wallet_1", debit.WalletID)
	assert.Equal(t, "wallet_2", credit.WalletID)
	assert.Equal(t, int64(100), debit.Amount)
	assert.Equal(t, int64(100), credit.Amount)
}

func TestTransferService_IdempotentReplay(t *testing.T) {
	uow := newTestUnitOfWork(t)
	seedWallet(t, uow, "wallet_1", 1000)
	seedWallet(t, uow, "wallet_2", 0)
	svc := service.NewTransferService(uow)

	req := service.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := svc.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, first.Replayed)

	second, err := svc.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, second.Replayed)
	assert.Equal(t, first.Transfer.ID, second.Transfer.ID)

	from, err := service.NewWalletService(uow).GetWallet(context.Background(), "wallet_1")
	require.NoError(t, err)
	assert.Equal(t, int64(900), from.Balance, "balance must only be debited once")

	_, entries, err := svc.GetTransfer(context.Background(), first.Transfer.ID)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "duplicate request must not create duplicate ledger entries")
}

func TestTransferService_IdempotencyKeyConflict(t *testing.T) {
	uow := newTestUnitOfWork(t)
	seedWallet(t, uow, "wallet_1", 1000)
	seedWallet(t, uow, "wallet_2", 0)
	svc := service.NewTransferService(uow)

	_, err := svc.Execute(context.Background(), service.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	require.NoError(t, err)

	_, err = svc.Execute(context.Background(), service.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200, // different payload, same key
	})
	assert.ErrorIs(t, err, domain.ErrIdempotencyKeyConflict)
}

func TestTransferService_InsufficientFunds(t *testing.T) {
	uow := newTestUnitOfWork(t)
	seedWallet(t, uow, "wallet_1", 50)
	seedWallet(t, uow, "wallet_2", 0)
	svc := service.NewTransferService(uow)

	result, err := svc.Execute(context.Background(), service.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TransferFailed, result.Transfer.Status)
	assert.NotEmpty(t, result.Transfer.FailureReason)

	from, err := service.NewWalletService(uow).GetWallet(context.Background(), "wallet_1")
	require.NoError(t, err)
	assert.Equal(t, int64(50), from.Balance, "balance must be unchanged on a failed transfer")

	_, entries, err := svc.GetTransfer(context.Background(), result.Transfer.ID)
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed transfer must not produce ledger entries")
}

func TestTransferService_ValidationErrors(t *testing.T) {
	uow := newTestUnitOfWork(t)
	seedWallet(t, uow, "wallet_1", 1000)
	svc := service.NewTransferService(uow)

	_, err := svc.Execute(context.Background(), service.TransferRequest{
		FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	})
	assert.ErrorIs(t, err, domain.ErrIdempotencyKeyRequired)

	_, err = svc.Execute(context.Background(), service.TransferRequest{
		IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 0,
	})
	assert.ErrorIs(t, err, domain.ErrInvalidAmount)

	_, err = svc.Execute(context.Background(), service.TransferRequest{
		IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 100,
	})
	assert.ErrorIs(t, err, domain.ErrSameWallet)

	_, err = svc.Execute(context.Background(), service.TransferRequest{
		IdempotencyKey: "k", FromWalletID: "wallet_1", ToWalletID: "no_such_wallet", Amount: 100,
	})
	assert.ErrorIs(t, err, domain.ErrWalletNotFound)
}

func TestTransferService_ConcurrentTransfersFromSameWallet(t *testing.T) {
	uow := newTestUnitOfWork(t)
	seedWallet(t, uow, "wallet_1", 1000)
	seedWallet(t, uow, "wallet_2", 0)
	svc := service.NewTransferService(uow)

	const n = 20
	const amount = 100 // n * amount > initial balance, so some must fail cleanly

	var wg sync.WaitGroup
	results := make([]*service.TransferResult, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.Execute(context.Background(), service.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-key-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
		}(i)
	}
	wg.Wait()

	var processed, failed int
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		switch results[i].Transfer.Status {
		case domain.TransferProcessed:
			processed++
		case domain.TransferFailed:
			failed++
		default:
			t.Fatalf("unexpected status %s", results[i].Transfer.Status)
		}
	}

	from, err := service.NewWalletService(uow).GetWallet(context.Background(), "wallet_1")
	require.NoError(t, err)
	to, err := service.NewWalletService(uow).GetWallet(context.Background(), "wallet_2")
	require.NoError(t, err)

	assert.Equal(t, int64(1000-processed*amount), from.Balance)
	assert.Equal(t, int64(processed*amount), to.Balance)
	assert.GreaterOrEqual(t, from.Balance, int64(0), "wallet must never go negative")
	assert.Equal(t, n, processed+failed, "every request must reach a terminal state")
}

func TestTransferService_ConcurrentIdenticalRequestsAreNotDuplicated(t *testing.T) {
	uow := newTestUnitOfWork(t)
	seedWallet(t, uow, "wallet_1", 1000)
	seedWallet(t, uow, "wallet_2", 0)
	svc := service.NewTransferService(uow)

	const n = 10
	req := service.TransferRequest{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	var wg sync.WaitGroup
	results := make([]*service.TransferResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.Execute(context.Background(), req)
		}(i)
	}
	wg.Wait()

	transferIDs := map[string]bool{}
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		transferIDs[results[i].Transfer.ID] = true
	}
	assert.Len(t, transferIDs, 1, "concurrent duplicate requests must resolve to a single transfer")

	from, err := service.NewWalletService(uow).GetWallet(context.Background(), "wallet_1")
	require.NoError(t, err)
	assert.Equal(t, int64(900), from.Balance, "duplicate concurrent requests must debit exactly once")
}

func TestTransferService_TransferNotFound(t *testing.T) {
	uow := newTestUnitOfWork(t)
	svc := service.NewTransferService(uow)
	_, _, err := svc.GetTransfer(context.Background(), "does-not-exist")
	assert.True(t, errors.Is(err, domain.ErrTransferNotFound))
}
