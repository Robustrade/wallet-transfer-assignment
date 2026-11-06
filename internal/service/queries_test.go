package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

func TestGetTransfer_ReturnsTheStoredOutcome(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})
	ctx := context.Background()
	created, err := f.svc.CreateTransfer(ctx, request(newKey(), "wallet_1", "wallet_2", 500))
	require.NoError(t, err)

	got, err := f.svc.GetTransfer(ctx, created.Transfer.ID)

	require.NoError(t, err)
	require.Equal(t, created.Transfer.ID, got.ID)
	require.Equal(t, created.Transfer.IdempotencyKey, got.IdempotencyKey)
	require.Equal(t, domain.StatusFailed, got.Status)
	require.Equal(t, domain.FailureInsufficientFunds, got.FailureReason)

	_, err = f.svc.GetTransfer(ctx, uuid.New())
	require.ErrorIs(t, err, domain.ErrTransferNotFound)
}

func TestGetWallet_ReflectsCompletedTransfers(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})
	ctx := context.Background()
	wallets := service.NewWalletService(postgres.New(f.pool, postgres.Options{}).Wallets())
	_, err := f.svc.CreateTransfer(ctx, request(newKey(), "wallet_1", "wallet_2", 40))
	require.NoError(t, err)

	w, err := wallets.GetWallet(ctx, "wallet_1")

	require.NoError(t, err)
	require.Equal(t, int64(60), w.Balance)
	_, err = wallets.GetWallet(ctx, "missing")
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
}
