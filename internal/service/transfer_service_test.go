package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

func TestCreateTransfer_MovesMoneyAndWritesABalancedLedgerPair(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})

	res, err := f.svc.CreateTransfer(context.Background(), request(newKey(), "wallet_1", "wallet_2", 30))

	require.NoError(t, err)
	require.False(t, res.Replayed)
	require.Equal(t, domain.StatusProcessed, res.Transfer.Status)
	require.Equal(t, int64(70), f.balance(t, "wallet_1"))
	require.Equal(t, int64(30), f.balance(t, "wallet_2"))
	require.Equal(t, []ledgerRow{
		{WalletID: "wallet_1", Type: "DEBIT", Amount: 30},
		{WalletID: "wallet_2", Type: "CREDIT", Amount: 30},
	}, f.ledgerFor(t, res.Transfer.ID))
	f.requireLedgerConsistent(t)
}

func TestCreateTransfer_CanSpendTheExactBalance(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})

	res, err := f.svc.CreateTransfer(context.Background(), request(newKey(), "wallet_1", "wallet_2", 100))

	require.NoError(t, err)
	require.Equal(t, domain.StatusProcessed, res.Transfer.Status)
	require.Zero(t, f.balance(t, "wallet_1"))
	f.requireLedgerConsistent(t)
}

func TestCreateTransfer_InsufficientFundsRecordsAFailedTransferAndMovesNoMoney(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})

	res, err := f.svc.CreateTransfer(context.Background(), request(newKey(), "wallet_1", "wallet_2", 101))

	require.NoError(t, err, "insufficient funds is a recorded business outcome")
	require.Equal(t, domain.StatusFailed, res.Transfer.Status)
	require.Equal(t, domain.FailureInsufficientFunds, res.Transfer.FailureReason)
	require.Equal(t, int64(100), f.balance(t, "wallet_1"))
	require.Equal(t, int64(0), f.balance(t, "wallet_2"))
	require.Empty(t, f.ledgerFor(t, res.Transfer.ID))
	require.Equal(t, 1, f.count(t, "transfers"))
	f.requireLedgerConsistent(t)
}

func TestCreateTransfer_SameKeyReplaysTheOriginalResultWithoutSideEffects(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})
	ctx := context.Background()
	req := request(newKey(), "wallet_1", "wallet_2", 30)

	first, err := f.svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	second, err := f.svc.CreateTransfer(ctx, req) // e.g. client retry after a lost response

	require.NoError(t, err)
	require.True(t, second.Replayed)
	require.Equal(t, req.IdempotencyKey, second.Transfer.IdempotencyKey)
	require.True(t, first.Transfer.CreatedAt.Equal(second.Transfer.CreatedAt), "replayed timestamps must match exactly")
	require.True(t, first.Transfer.UpdatedAt.Equal(second.Transfer.UpdatedAt), "replayed timestamps must match exactly")
	first.Transfer.CreatedAt, first.Transfer.UpdatedAt = second.Transfer.CreatedAt, second.Transfer.UpdatedAt // ignore time.Location
	require.Equal(t, first.Transfer, second.Transfer, "a replay returns the original transfer unchanged")
	require.Equal(t, int64(70), f.balance(t, "wallet_1"), "money moves exactly once")
	require.Equal(t, 1, f.count(t, "transfers"))
	require.Equal(t, 2, f.count(t, "ledger_entries"))
	f.requireLedgerConsistent(t)
}

func TestCreateTransfer_ReplayOfAFailedTransferStaysFailedEvenAfterFunding(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 10, "wallet_2": 0})
	ctx := context.Background()
	req := request(newKey(), "wallet_1", "wallet_2", 50)

	first, err := f.svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.Equal(t, domain.StatusFailed, first.Transfer.Status)

	_, err = f.pool.Exec(ctx, `UPDATE wallets SET balance = 1000 WHERE id = 'wallet_1'`)
	require.NoError(t, err)
	f.opening["wallet_1"] = 1000

	again, err := f.svc.CreateTransfer(ctx, req)

	require.NoError(t, err)
	require.True(t, again.Replayed)
	require.Equal(t, first.Transfer.ID, again.Transfer.ID)
	require.Equal(t, domain.StatusFailed, again.Transfer.Status, "a key's outcome is final; use a new key to retry")
	require.Equal(t, int64(1000), f.balance(t, "wallet_1"))
	f.requireLedgerConsistent(t)
}

func TestCreateTransfer_ReusingAKeyWithADifferentPayloadIsRejected(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0, "wallet_3": 0})
	ctx := context.Background()
	key := newKey()
	_, err := f.svc.CreateTransfer(ctx, request(key, "wallet_1", "wallet_2", 30))
	require.NoError(t, err)

	for name, changed := range map[string]domain.TransferRequest{
		"different amount":      request(key, "wallet_1", "wallet_2", 31),
		"different destination": request(key, "wallet_1", "wallet_3", 30),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.CreateTransfer(ctx, changed)
			require.ErrorIs(t, err, domain.ErrIdempotencyKeyReused)
		})
	}
	require.Equal(t, 1, f.count(t, "transfers"))
	require.Equal(t, int64(70), f.balance(t, "wallet_1"))
	f.requireLedgerConsistent(t)
}

func TestCreateTransfer_UnknownWalletPersistsNothingSoTheKeyCanBeRetried(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100})
	ctx := context.Background()
	req := request(newKey(), "wallet_1", "wallet_new", 30)

	_, err := f.svc.CreateTransfer(ctx, req)

	require.ErrorIs(t, err, domain.ErrWalletNotFound)
	require.Zero(t, f.count(t, "transfers"))
	require.Zero(t, f.count(t, "idempotency_records"), "a rolled-back attempt must not consume the key")

	_, err = f.pool.Exec(ctx, `INSERT INTO wallets (id, balance) VALUES ('wallet_new', 0)`)
	require.NoError(t, err)
	f.opening["wallet_new"] = 0

	res, err := f.svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.False(t, res.Replayed)
	require.Equal(t, domain.StatusProcessed, res.Transfer.Status)
	f.requireLedgerConsistent(t)
}

func TestCreateTransfer_InvalidRequestsAreRejectedBeforeTouchingTheDatabase(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})

	_, err := f.svc.CreateTransfer(context.Background(), request(newKey(), "wallet_1", "wallet_2", 0))

	var verr *domain.ValidationError
	require.True(t, errors.As(err, &verr), "got %v", err)
	require.Zero(t, f.count(t, "transfers"))
	require.Zero(t, f.count(t, "idempotency_records"))
}
