package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
)

func input(key, from, to string, amount domain.Money) TransferInput {
	return TransferInput{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

func TestTransfer_HappyPath(t *testing.T) {
	h := newHarness()
	h.wallets.seed("alice", 1000)
	h.wallets.seed("bob", 500)

	res, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 300))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.Replayed)
	assert.Equal(t, domain.StatusProcessed, res.Transfer.Status)

	// Balances moved correctly.
	aliceBal, err := h.svc.GetWalletBalance(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, domain.Money(700), aliceBal.Balance)
	bobBal, err := h.svc.GetWalletBalance(context.Background(), "bob")
	require.NoError(t, err)
	assert.Equal(t, domain.Money(800), bobBal.Balance)

	// Exactly two ledger entries: a DEBIT on source and a CREDIT on dest, equal.
	entries := h.ledger.all()
	require.Len(t, entries, 2)
	var debit, credit domain.LedgerEntry
	for _, e := range entries {
		switch e.Type {
		case domain.EntryDebit:
			debit = e
		case domain.EntryCredit:
			credit = e
		}
	}
	assert.Equal(t, "alice", debit.WalletID)
	assert.Equal(t, domain.Money(300), debit.Amount)
	assert.Equal(t, "bob", credit.WalletID)
	assert.Equal(t, domain.Money(300), credit.Amount)
	assert.Equal(t, res.Transfer.ID, debit.TransferID)
	assert.Equal(t, res.Transfer.ID, credit.TransferID)

	// Idempotency record completed.
	rec, err := h.idempotency.Get(context.Background(), "k1")
	require.NoError(t, err)
	assert.Equal(t, domain.IdempotencyCompleted, rec.Status)
	assert.Equal(t, res.Transfer.ID, rec.TransferID)
	assert.NotEmpty(t, rec.ResponseBody)
}

func TestTransfer_IdempotentReplay(t *testing.T) {
	h := newHarness()
	h.wallets.seed("alice", 1000)
	h.wallets.seed("bob", 0)

	in := input("k1", "alice", "bob", 250)
	first, err := h.svc.Transfer(context.Background(), in)
	require.NoError(t, err)
	assert.False(t, first.Replayed)

	second, err := h.svc.Transfer(context.Background(), in)
	require.NoError(t, err)
	assert.True(t, second.Replayed)
	assert.Equal(t, first.Transfer.ID, second.Transfer.ID)

	// Executed exactly once: balances reflect a single transfer.
	aliceBal, _ := h.svc.GetWalletBalance(context.Background(), "alice")
	assert.Equal(t, domain.Money(750), aliceBal.Balance)
	bobBal, _ := h.svc.GetWalletBalance(context.Background(), "bob")
	assert.Equal(t, domain.Money(250), bobBal.Balance)

	// Still exactly two ledger entries.
	assert.Len(t, h.ledger.all(), 2)
}

func TestTransfer_KeyReuseDifferentPayload(t *testing.T) {
	h := newHarness()
	h.wallets.seed("alice", 1000)
	h.wallets.seed("bob", 0)

	_, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 100))
	require.NoError(t, err)

	// Same key, different amount.
	res, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 200))
	assert.Nil(t, res)
	assert.ErrorIs(t, err, domain.ErrIdempotencyKeyReuse)

	// Original transfer unaffected.
	aliceBal, _ := h.svc.GetWalletBalance(context.Background(), "alice")
	assert.Equal(t, domain.Money(900), aliceBal.Balance)
}

func TestTransfer_InsufficientFunds(t *testing.T) {
	h := newHarness()
	h.wallets.seed("alice", 100)
	h.wallets.seed("bob", 50)

	res, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 500))
	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	require.NotNil(t, res)
	assert.False(t, res.Replayed)
	assert.Equal(t, domain.StatusFailed, res.Transfer.Status)
	assert.Equal(t, "insufficient funds", res.Transfer.FailureReason)

	// Balances unchanged.
	aliceBal, _ := h.svc.GetWalletBalance(context.Background(), "alice")
	assert.Equal(t, domain.Money(100), aliceBal.Balance)
	bobBal, _ := h.svc.GetWalletBalance(context.Background(), "bob")
	assert.Equal(t, domain.Money(50), bobBal.Balance)

	// No ledger entries written.
	assert.Empty(t, h.ledger.all())

	// Durable FAILED transfer exists and is fetchable.
	got, err := h.svc.GetTransfer(context.Background(), res.Transfer.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, got.Status)

	// Duplicate of the failed request replays the same FAILED outcome.
	replay, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 500))
	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	require.NotNil(t, replay)
	assert.True(t, replay.Replayed)
	assert.Equal(t, res.Transfer.ID, replay.Transfer.ID)
	assert.Equal(t, domain.StatusFailed, replay.Transfer.Status)
}

func TestTransfer_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		in      TransferInput
		wantErr error
	}{
		{"missing key", input("", "alice", "bob", 100), domain.ErrMissingIdempotencyKey},
		{"missing from", input("k1", "", "bob", 100), domain.ErrMissingWalletID},
		{"missing to", input("k1", "alice", "", 100), domain.ErrMissingWalletID},
		{"same wallet", input("k1", "alice", "alice", 100), domain.ErrSameWallet},
		{"zero amount", input("k1", "alice", "bob", 0), domain.ErrInvalidAmount},
		{"negative amount", input("k1", "alice", "bob", -5), domain.ErrInvalidAmount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			h.wallets.seed("alice", 1000)
			h.wallets.seed("bob", 1000)

			res, err := h.svc.Transfer(context.Background(), tc.in)
			assert.Nil(t, res)
			assert.ErrorIs(t, err, tc.wantErr)

			// No side effects.
			aliceBal, _ := h.svc.GetWalletBalance(context.Background(), "alice")
			assert.Equal(t, domain.Money(1000), aliceBal.Balance)
			assert.Empty(t, h.ledger.all())
			_, idemErr := h.idempotency.Get(context.Background(), tc.in.IdempotencyKey)
			assert.Error(t, idemErr)
		})
	}
}

func TestTransfer_WalletNotFound(t *testing.T) {
	h := newHarness()
	h.wallets.seed("alice", 1000)
	// bob does not exist.

	res, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 100))
	assert.Nil(t, res)
	assert.ErrorIs(t, err, domain.ErrWalletNotFound)

	// Nothing durable persisted: no ledger and balance unchanged.
	assert.Empty(t, h.ledger.all())
	aliceBal, _ := h.svc.GetWalletBalance(context.Background(), "alice")
	assert.Equal(t, domain.Money(1000), aliceBal.Balance)
}

func TestTransfer_OpposingTransfersBothSucceed(t *testing.T) {
	h := newHarness()
	h.wallets.seed("alice", 1000)
	h.wallets.seed("bob", 1000)

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	go func() {
		defer wg.Done()
		_, errs[0] = h.svc.Transfer(context.Background(), input("ka", "alice", "bob", 100))
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = h.svc.Transfer(context.Background(), input("kb", "bob", "alice", 200))
	}()
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	aliceBal, _ := h.svc.GetWalletBalance(context.Background(), "alice")
	bobBal, _ := h.svc.GetWalletBalance(context.Background(), "bob")
	// Net effect of the two opposing transfers leaves alice up by 100 and bob
	// down by 100, with no lost updates.
	assert.Equal(t, domain.Money(1100), aliceBal.Balance)
	assert.Equal(t, domain.Money(900), bobBal.Balance)
}

func TestTransfer_RepositoryErrorsRollBack(t *testing.T) {
	boom := errors.New("boom")

	t.Run("transfer create fails", func(t *testing.T) {
		h := newHarness()
		h.wallets.seed("alice", 1000)
		h.wallets.seed("bob", 0)
		h.transfers.createErr = boom

		res, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 100))
		assert.Nil(t, res)
		assert.ErrorIs(t, err, boom)
		assert.Empty(t, h.ledger.all())
	})

	t.Run("ledger create fails", func(t *testing.T) {
		h := newHarness()
		h.wallets.seed("alice", 1000)
		h.wallets.seed("bob", 0)
		h.ledger.createErr = boom

		res, err := h.svc.Transfer(context.Background(), input("k1", "alice", "bob", 100))
		assert.Nil(t, res)
		assert.ErrorIs(t, err, boom)
	})
}

func TestGetTransfer_NotFound(t *testing.T) {
	h := newHarness()
	_, err := h.svc.GetTransfer(context.Background(), "nope")
	assert.ErrorIs(t, err, domain.ErrTransferNotFound)
}

func TestGetWalletBalance_NotFound(t *testing.T) {
	h := newHarness()
	_, err := h.svc.GetWalletBalance(context.Background(), "nope")
	assert.ErrorIs(t, err, domain.ErrWalletNotFound)
}

// Guard: ensure errors.Is semantics hold for the wrapped sentinel checks the
// service relies on (defensive, in case domain errors are ever wrapped).
func TestSentinelWrapping(t *testing.T) {
	wrapped := errors.Join(domain.ErrInsufficientFunds, errors.New("ctx"))
	assert.ErrorIs(t, wrapped, domain.ErrInsufficientFunds)
}
