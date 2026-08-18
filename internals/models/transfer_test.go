package models

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewTransfer_Valid(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	tr, err := NewTransfer(from, to, 100, "key-1")
	require.NoError(t, err)
	require.Equal(t, TransferPending, tr.State)
	require.NotEqual(t, uuid.Nil, tr.ID)
	require.Equal(t, from, tr.FromWalletID)
	require.Equal(t, to, tr.ToWalletID)
	require.EqualValues(t, 100, tr.Amount)
	require.Equal(t, "key-1", tr.IdempotencyKey)
}

func TestNewTransfer_SameWallet(t *testing.T) {
	same := uuid.New()
	_, err := NewTransfer(same, same, 100, "key-1")
	require.ErrorIs(t, err, ErrSameWallet)
}

func TestNewTransfer_NonPositiveAmount(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	for _, amount := range []int64{0, -100} {
		_, err := NewTransfer(from, to, amount, "key-1")
		require.ErrorIs(t, err, ErrNonPositiveAmount)
	}
}

func TestNewTransfer_EmptyIdempotencyKey(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	_, err := NewTransfer(from, to, 100, "")
	require.Error(t, err)
}

func TestMarkProcessed_FromPending(t *testing.T) {
	tr, err := NewTransfer(uuid.New(), uuid.New(), 100, "key-1")
	require.NoError(t, err)

	require.NoError(t, tr.MarkProcessed())
	require.Equal(t, TransferProcessed, tr.State)
}

func TestMarkProcessed_RejectsNonPendingState(t *testing.T) {
	for _, start := range []TransferState{TransferProcessed, TransferFailed} {
		tr := &Transfer{State: start}
		err := tr.MarkProcessed()
		require.ErrorIs(t, err, ErrInvalidTransition)
		require.Equal(t, start, tr.State, "state must be unchanged on a rejected transition")
	}
}

func TestMarkFailed_FromPending(t *testing.T) {
	tr, err := NewTransfer(uuid.New(), uuid.New(), 100, "key-1")
	require.NoError(t, err)

	require.NoError(t, tr.MarkFailed("insufficient balance"))
	require.Equal(t, TransferFailed, tr.State)
	require.Equal(t, "insufficient balance", tr.FailureReason)
}

func TestMarkFailed_RejectsNonPendingState(t *testing.T) {
	for _, start := range []TransferState{TransferProcessed, TransferFailed} {
		tr := &Transfer{State: start}
		err := tr.MarkFailed("some reason")
		require.ErrorIs(t, err, ErrInvalidTransition)
		require.Equal(t, start, tr.State, "state must be unchanged on a rejected transition")
		require.Empty(t, tr.FailureReason)
	}
}

func TestIsTerminal(t *testing.T) {
	cases := []struct {
		state    TransferState
		terminal bool
	}{
		{TransferPending, false},
		{TransferProcessed, true},
		{TransferFailed, true},
	}
	for _, c := range cases {
		tr := &Transfer{State: c.state}
		require.Equal(t, c.terminal, tr.IsTerminal(), "state %s", c.state)
	}
}
