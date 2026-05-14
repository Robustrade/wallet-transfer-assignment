package service_test

import (
	"context"
	"testing"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── CreateWallet ──────────────────────────────────────────────────────────────

func TestCreateWallet_Success(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	w, err := svc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             unique("w"),
		InitialBalance: 1000,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, w.ID)
	assert.Equal(t, int64(1000), w.Balance)
	assert.NotZero(t, w.CreatedAt)
}

func TestCreateWallet_GeneratesID(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	w, err := svc.CreateWallet(ctx, service.CreateWalletRequest{
		InitialBalance: 0,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, w.ID) // UUID generated when no ID supplied
}

func TestCreateWallet_ZeroBalance(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	w, err := svc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             unique("w"),
		InitialBalance: 0,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(0), w.Balance)
}

// ── GetWallet ─────────────────────────────────────────────────────────────────

func TestGetWallet_Success(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	created := seedWallet(t, svc, unique("w"), 250)

	got, err := svc.GetWallet(ctx, created.ID)

	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, int64(250), got.Balance)
}

func TestGetWallet_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.GetWallet(ctx, "non-existent-wallet")

	assert.ErrorIs(t, err, domain.ErrWalletNotFound)
}

// ── GetTransferHistory ────────────────────────────────────────────────────────

func TestGetTransferHistory_ReturnsTransfersForWallet(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	from := seedWallet(t, svc, unique("from"), 500)
	to := seedWallet(t, svc, unique("to"), 0)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
	})
	require.NoError(t, err)

	transfers, err := svc.GetTransferHistory(ctx, from.ID)

	require.NoError(t, err)
	require.Len(t, transfers, 1)
	assert.Equal(t, from.ID, transfers[0].FromWalletID)
	assert.Equal(t, to.ID, transfers[0].ToWalletID)
	assert.Equal(t, int64(100), transfers[0].Amount)
}

func TestGetTransferHistory_AppearsForBothWallets(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	from := seedWallet(t, svc, unique("from"), 500)
	to := seedWallet(t, svc, unique("to"), 0)

	_, err := svc.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: unique("idem"),
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
	})
	require.NoError(t, err)

	// Transfer must appear in both wallets' histories.
	fromHistory, err := svc.GetTransferHistory(ctx, from.ID)
	require.NoError(t, err)
	assert.Len(t, fromHistory, 1)

	toHistory, err := svc.GetTransferHistory(ctx, to.ID)
	require.NoError(t, err)
	assert.Len(t, toHistory, 1)

	assert.Equal(t, fromHistory[0].ID, toHistory[0].ID)
}

func TestGetTransferHistory_Empty(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	w := seedWallet(t, svc, unique("w"), 100)

	transfers, err := svc.GetTransferHistory(ctx, w.ID)

	require.NoError(t, err)
	assert.Empty(t, transfers)
}
