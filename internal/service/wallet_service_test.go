package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

func TestWalletService_Create(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	svc := service.NewWalletService(uow)

	w, err := svc.Create(ctx, "wallet_1", "INR")
	require.NoError(t, err)
	require.Equal(t, "wallet_1", w.ID)
	require.Equal(t, domain.WalletKindUser, w.Kind)
	require.Zero(t, w.BalanceMinor)

	_, err = svc.Create(ctx, "wallet_1", "INR")
	require.ErrorIs(t, err, domain.ErrWalletAlreadyExists)
}

func TestWalletService_Create_InvalidInput(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	svc := service.NewWalletService(uow)

	_, err := svc.Create(ctx, "bad id!", "INR")
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve)
}

func TestWalletService_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	svc := service.NewWalletService(uow)

	_, err := svc.Get(ctx, "missing")
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
}

func TestWalletService_ListEntries(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 1000)
	seedWallet(t, uow, "wallet_2", "INR", 0)

	transferSvc := service.NewTransferService(uow)
	_, err := transferSvc.Create(ctx, domain.TransferCommand{IdempotencyKey: "k1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100})
	require.NoError(t, err)

	walletSvc := service.NewWalletService(uow)
	entries, err := walletSvc.ListEntries(ctx, "wallet_1", 10, nil)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, domain.LedgerEntryDebit, entries[0].EntryType)

	_, err = walletSvc.ListEntries(ctx, "missing", 10, nil)
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
}
