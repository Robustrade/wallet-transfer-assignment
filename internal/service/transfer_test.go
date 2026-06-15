package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/repository"
	"github.com/wallet-transfer-assignment/internal/service"
	"github.com/wallet-transfer-assignment/testutil"
)

// newTestService wires a TransferService against the test database pool.
func newTestService(t *testing.T) (*service.TransferService, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestPool(t)
	walletRepo := repository.NewWalletRepository(pool)
	transferRepo := repository.NewTransferRepository(pool)
	ledgerRepo := repository.NewLedgerEntryRepository(pool)
	svc := service.NewTransferService(pool, walletRepo, transferRepo, ledgerRepo)
	return svc, pool
}

func TestCreateTransfer_Success(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "alice", 1000)
	testutil.SeedWallet(t, pool, "bob", 500)

	tr, err := svc.CreateTransfer(context.Background(), service.TransferRequest{
		IdempotencyKey: "tx-success-1",
		FromWalletID:   "alice",
		ToWalletID:     "bob",
		Amount:         300,
	})

	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessed, tr.Status)
	assert.Equal(t, int64(300), tr.Amount)

	assert.Equal(t, int64(700), testutil.WalletBalance(t, pool, "alice"))
	assert.Equal(t, int64(800), testutil.WalletBalance(t, pool, "bob"))

	// Exactly two ledger entries must exist for the transfer.
	assert.Equal(t, 2, testutil.LedgerEntryCount(t, pool, tr.ID.String()))
}

func TestCreateTransfer_Idempotency(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "alice", 1000)
	testutil.SeedWallet(t, pool, "bob", 0)

	req := service.TransferRequest{
		IdempotencyKey: "tx-idempotent-1",
		FromWalletID:   "alice",
		ToWalletID:     "bob",
		Amount:         200,
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, domain.StatusProcessed, first.Status)

	// Identical second call must return the same transfer — no side effects.
	second, err := svc.CreateTransfer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, first.Status, second.Status)

	// Balances must only change once — no double-spend.
	assert.Equal(t, int64(800), testutil.WalletBalance(t, pool, "alice"))
	assert.Equal(t, int64(200), testutil.WalletBalance(t, pool, "bob"))

	// Still exactly two ledger entries after two calls.
	assert.Equal(t, 2, testutil.LedgerEntryCount(t, pool, first.ID.String()))
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "alice", 100)
	testutil.SeedWallet(t, pool, "bob", 0)

	tr, err := svc.CreateTransfer(context.Background(), service.TransferRequest{
		IdempotencyKey: "tx-insufficient-1",
		FromWalletID:   "alice",
		ToWalletID:     "bob",
		Amount:         500,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)

	// The transfer record is persisted with FAILED status for auditability.
	require.NotNil(t, tr)
	assert.Equal(t, domain.StatusFailed, tr.Status)

	// Balances must be unchanged.
	assert.Equal(t, int64(100), testutil.WalletBalance(t, pool, "alice"))
	assert.Equal(t, int64(0), testutil.WalletBalance(t, pool, "bob"))
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "alice", 1000)

	_, err := svc.CreateTransfer(context.Background(), service.TransferRequest{
		IdempotencyKey: "tx-notwallet-1",
		FromWalletID:   "alice",
		ToWalletID:     "does-not-exist",
		Amount:         100,
	})

	require.Error(t, err)
}

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	svc, _ := newTestService(t)

	tests := []struct {
		name    string
		req     service.TransferRequest
		wantErr error
	}{
		{
			name:    "missing idempotency key",
			req:     service.TransferRequest{FromWalletID: "a", ToWalletID: "b", Amount: 10},
			wantErr: domain.ErrMissingIdempotency,
		},
		{
			name:    "same wallet",
			req:     service.TransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "a", Amount: 10},
			wantErr: domain.ErrSameWallet,
		},
		{
			name:    "zero amount",
			req:     service.TransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 0},
			wantErr: domain.ErrAmountNotPositive,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateTransfer(context.Background(), tc.req)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestCreateTransfer_ConcurrentSafetyOnSameWallet(t *testing.T) {
	svc, pool := newTestService(t)

	const (
		initialBalance = int64(10000)
		transferAmount = int64(100)
		workers        = 20
	)

	testutil.SeedWallet(t, pool, "source", initialBalance)
	testutil.SeedWallet(t, pool, "sink", 0)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
	)

	for i := range workers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(context.Background(), service.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", n),
				FromWalletID:   "source",
				ToWalletID:     "sink",
				Amount:         transferAmount,
			})
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	sourceBalance := testutil.WalletBalance(t, pool, "source")
	sinkBalance := testutil.WalletBalance(t, pool, "sink")

	// Total money in the system must be conserved regardless of how many
	// transfers succeeded or failed.
	assert.Equal(t, initialBalance, sourceBalance+sinkBalance,
		"total money must be conserved")
	assert.Equal(t, int64(successes)*transferAmount, sinkBalance,
		"sink balance must equal successful-transfer count × amount")
}
