package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

func seedWallet(t *testing.T, uow *fakeUnitOfWork, id, currency string, balance int64) {
	t.Helper()
	w, err := domain.NewWallet(id, currency)
	require.NoError(t, err)
	w.BalanceMinor = balance
	require.NoError(t, uow.Wallets().Create(context.Background(), w))
}

func TestTransferService_Create_HappyPath(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 1000)
	seedWallet(t, uow, "wallet_2", "INR", 0)
	svc := service.NewTransferService(uow)

	result, err := svc.Create(ctx, domain.TransferCommand{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		AmountMinor:    100,
	})
	require.NoError(t, err)
	require.Equal(t, service.OutcomeCreated, result.Outcome)
	require.Equal(t, domain.TransferStatusProcessed, result.Transfer.Status)
	require.NotNil(t, result.Transfer.SettledAt)

	from, err := uow.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(900), from.BalanceMinor)

	to, err := uow.Wallets().Get(ctx, "wallet_2")
	require.NoError(t, err)
	require.Equal(t, int64(100), to.BalanceMinor)

	debitEntries, err := uow.Ledger().ListByWallet(ctx, "wallet_1", 10)
	require.NoError(t, err)
	require.Len(t, debitEntries, 1)
	require.Equal(t, domain.LedgerEntryDebit, debitEntries[0].EntryType)
	require.Equal(t, int64(900), debitEntries[0].BalanceAfter)

	creditEntries, err := uow.Ledger().ListByWallet(ctx, "wallet_2", 10)
	require.NoError(t, err)
	require.Len(t, creditEntries, 1)
	require.Equal(t, domain.LedgerEntryCredit, creditEntries[0].EntryType)
	require.Equal(t, int64(100), creditEntries[0].BalanceAfter)
}

func TestTransferService_Create_ReplayIdenticalRequest(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 1000)
	seedWallet(t, uow, "wallet_2", "INR", 0)
	svc := service.NewTransferService(uow)

	cmd := domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100}

	first, err := svc.Create(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, service.OutcomeCreated, first.Outcome)

	second, err := svc.Create(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, service.OutcomeReplayed, second.Outcome)
	require.Equal(t, first.Transfer, second.Transfer, "a replay must return the identical transfer")

	from, err := uow.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(900), from.BalanceMinor, "balance must move exactly once")

	entries, err := uow.Ledger().ListByWallet(ctx, "wallet_1", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the ledger must not gain a second entry on replay")
}

func TestTransferService_Create_KeyReuseWithDifferentPayload(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 1000)
	seedWallet(t, uow, "wallet_2", "INR", 0)
	svc := service.NewTransferService(uow)

	_, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100})
	require.NoError(t, err)

	_, err = svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 200})
	require.ErrorIs(t, err, domain.ErrIdempotencyKeyReuse)

	from, err := uow.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(900), from.BalanceMinor, "the original transfer must be untouched by the rejected reuse")
}

func TestTransferService_Create_InsufficientFunds(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 50)
	seedWallet(t, uow, "wallet_2", "INR", 0)
	svc := service.NewTransferService(uow)

	result, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100})
	require.NoError(t, err, "insufficient funds is a settled outcome, not a returned error")
	require.Equal(t, service.OutcomeCreated, result.Outcome)
	require.Equal(t, domain.TransferStatusFailed, result.Transfer.Status)
	require.Equal(t, domain.FailureCodeInsufficientFunds, result.Transfer.FailureCode)
	require.Nil(t, result.Transfer.SettledAt)

	from, err := uow.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(50), from.BalanceMinor, "balance must be untouched by a failed transfer")

	entries, err := uow.Ledger().ListByWallet(ctx, "wallet_1", 10)
	require.NoError(t, err)
	require.Empty(t, entries, "a failed transfer must produce zero ledger entries")

	replay, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100})
	require.NoError(t, err)
	require.Equal(t, service.OutcomeReplayed, replay.Outcome)
	require.Equal(t, domain.TransferStatusFailed, replay.Transfer.Status)
	require.Equal(t, result.Transfer, replay.Transfer, "a replay of a failed transfer must be stable")
}

func TestTransferService_Create_UnknownWallet(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 1000)
	svc := service.NewTransferService(uow)

	_, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_missing", AmountMinor: 100})
	require.ErrorIs(t, err, domain.ErrWalletNotFound)

	_, err = uow.Transfers().FindByIdempotencyKey(ctx, "abc123")
	require.ErrorIs(t, err, domain.ErrTransferNotFound, "no transfer should have been created")
}

func TestTransferService_Create_CurrencyMismatchBetweenWallets(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 1000)
	seedWallet(t, uow, "wallet_2", "USD", 0)
	svc := service.NewTransferService(uow)

	_, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100})
	require.ErrorIs(t, err, domain.ErrCurrencyMismatch)

	_, err = uow.Transfers().FindByIdempotencyKey(ctx, "abc123")
	require.ErrorIs(t, err, domain.ErrTransferNotFound)
}

func TestTransferService_Create_RequestedCurrencyMismatch(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	seedWallet(t, uow, "wallet_1", "INR", 1000)
	seedWallet(t, uow, "wallet_2", "INR", 0)
	svc := service.NewTransferService(uow)

	_, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100, Currency: "USD"})
	require.ErrorIs(t, err, domain.ErrCurrencyMismatch)
}

func TestTransferService_Create_SameWalletIsAValidationError(t *testing.T) {
	ctx := context.Background()
	uow := newFakeUnitOfWork()
	svc := service.NewTransferService(uow)

	_, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_1", AmountMinor: 100})
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve)
}

func TestTransferService_Create_RetriesTransientTransactionFailures(t *testing.T) {
	ctx := context.Background()
	base := newFakeUnitOfWork()
	seedWallet(t, base, "wallet_1", "INR", 1000)
	seedWallet(t, base, "wallet_2", "INR", 0)

	retrying := &retryingUnitOfWork{fakeUnitOfWork: base, failAttempts: 2}
	svc := service.NewTransferService(retrying)

	result, err := svc.Create(ctx, domain.TransferCommand{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", AmountMinor: 100})
	require.NoError(t, err)
	require.Equal(t, domain.TransferStatusProcessed, result.Transfer.Status)
	require.Equal(t, 3, retrying.attempts, "expected 2 failed attempts before the 3rd succeeded")

	from, err := base.Wallets().Get(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(900), from.BalanceMinor, "exactly one logical transfer despite 3 Do() attempts")

	entries, err := base.Ledger().ListByWallet(ctx, "wallet_1", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
