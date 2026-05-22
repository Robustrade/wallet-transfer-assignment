package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/domain"
	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/service"
)

func cmd(key, from, to string, amount int64) service.CreateTransferCommand {
	return service.CreateTransferCommand{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

// Happy path: money moves, balances are correct, status is PROCESSED, and the
// ledger records exactly one DEBIT and one CREDIT that net out per wallet.
func TestCreateTransfer_HappyPath(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	from, to := seedWallet(t, 100), seedWallet(t, 0)

	res, err := svc.CreateTransfer(ctx, cmd(uuid.NewString(), from, to, 60))
	require.NoError(t, err)
	require.Equal(t, "PROCESSED", res.Response.Status)
	require.False(t, res.Replayed)

	require.Equal(t, int64(40), walletBalance(t, from))
	require.Equal(t, int64(60), walletBalance(t, to))

	// Double-entry: ledger net equals the balance change.
	require.Equal(t, int64(-60), ledgerNet(t, from))
	require.Equal(t, int64(60), ledgerNet(t, to))
}

// Condition 4: a duplicate request (same key, same body) must not transfer
// twice. The second call replays the original result.
func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	from, to := seedWallet(t, 100), seedWallet(t, 0)
	key := uuid.NewString()

	first, err := svc.CreateTransfer(ctx, cmd(key, from, to, 30))
	require.NoError(t, err)

	second, err := svc.CreateTransfer(ctx, cmd(key, from, to, 30))
	require.NoError(t, err)

	require.True(t, second.Replayed)
	require.Equal(t, first.Response.ID, second.Response.ID) // same transfer
	require.Equal(t, int64(70), walletBalance(t, from))     // debited once, not twice
	require.Equal(t, 1, transferCountForKey(t, key))
}

// Condition 4: reusing a key with different parameters is a client error, not
// a replay, and must not create a second transfer.
func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	from, to := seedWallet(t, 100), seedWallet(t, 0)
	key := uuid.NewString()

	_, err := svc.CreateTransfer(ctx, cmd(key, from, to, 30))
	require.NoError(t, err)

	_, err = svc.CreateTransfer(ctx, cmd(key, from, to, 999)) // different amount
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)

	require.Equal(t, int64(70), walletBalance(t, from))
	require.Equal(t, 1, transferCountForKey(t, key))
}

// Condition 2: insufficient funds is recorded as a FAILED transfer with no
// balance change, and retrying the same key replays that FAILED result.
func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	from, to := seedWallet(t, 50), seedWallet(t, 0)
	key := uuid.NewString()

	res, err := svc.CreateTransfer(ctx, cmd(key, from, to, 100))
	require.NoError(t, err)
	require.Equal(t, "FAILED", res.Response.Status)
	require.Equal(t, int64(50), walletBalance(t, from)) // unchanged
	require.Equal(t, int64(0), walletBalance(t, to))
	require.Equal(t, int64(0), ledgerNet(t, from)) // no ledger entries for a failed transfer

	// Retry replays the FAILED outcome; no new transfer, no balance change.
	retry, err := svc.CreateTransfer(ctx, cmd(key, from, to, 100))
	require.NoError(t, err)
	require.True(t, retry.Replayed)
	require.Equal(t, "FAILED", retry.Response.Status)
	require.Equal(t, 1, transferCountForKey(t, key))
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	from := seedWallet(t, 100)

	_, err := svc.CreateTransfer(ctx, cmd(uuid.NewString(), from, uuid.NewString(), 10))
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
}

func TestCreateTransfer_Validation(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	w := seedWallet(t, 100)

	_, err := svc.CreateTransfer(ctx, cmd(uuid.NewString(), w, w, 10))
	require.ErrorIs(t, err, domain.ErrSameWallet)

	_, err = svc.CreateTransfer(ctx, cmd(uuid.NewString(), w, seedWallet(t, 0), 0))
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
}

// Conditions 1 + 3: under many concurrent debits of the same wallet, balances
// stay correct, no double spend occurs, and the wallet never goes negative.
//
// Setup: balance 100, 50 concurrent transfers of 10. Exactly 10 can succeed.
func TestCreateTransfer_ConcurrentDebits_NoDoubleSpend(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	from, to := seedWallet(t, 100), seedWallet(t, 0)

	const workers, amount = 50, 10
	var processed, failed atomicCounter

	runConcurrently(workers, func(int) {
		res, err := svc.CreateTransfer(ctx, cmd(uuid.NewString(), from, to, amount))
		require.NoError(t, err)
		switch res.Response.Status {
		case "PROCESSED":
			processed.inc()
		case "FAILED":
			failed.inc()
		}
	})

	require.Equal(t, int64(10), processed.get(), "exactly balance/amount transfers may succeed")
	require.Equal(t, int64(workers-10), failed.get())

	// No double spend, never negative, and the destination received exactly
	// what the source lost (money is conserved).
	require.Equal(t, int64(0), walletBalance(t, from))
	require.Equal(t, int64(100), walletBalance(t, to))
	require.GreaterOrEqual(t, walletBalance(t, from), int64(0))

	// Ledger reconciles with stored balances.
	require.Equal(t, int64(-100), ledgerNet(t, from))
	require.Equal(t, int64(100), ledgerNet(t, to))
}

// Condition 4: concurrent duplicate delivery of the SAME idempotency key must
// produce exactly one transfer; the rest replay it. Money moves once.
func TestCreateTransfer_ConcurrentSameKey_ExactlyOnce(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	svc := newService()
	from, to := seedWallet(t, 100), seedWallet(t, 0)
	key := uuid.NewString()

	const workers = 20
	ids := make([]string, workers)

	runConcurrently(workers, func(i int) {
		res, err := svc.CreateTransfer(ctx, cmd(key, from, to, 40))
		require.NoError(t, err)
		ids[i] = res.Response.ID
	})

	// Every caller saw the same transfer, and money moved exactly once.
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
	require.Equal(t, 1, transferCountForKey(t, key))
	require.Equal(t, int64(60), walletBalance(t, from))
	require.Equal(t, int64(40), walletBalance(t, to))
}
