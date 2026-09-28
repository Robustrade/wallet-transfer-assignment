package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedNow has sub-microsecond precision and a non-UTC zone so tests can
// check that NewTransfer normalises both.
var fixedNow = time.Date(2026, 9, 26, 18, 30, 0, 123456789, time.FixedZone("SGT", 8*60*60))

func newPending(t *testing.T) *Transfer {
	t.Helper()
	tr, err := NewTransfer("key-1", "wallet_1", "wallet_2", 100, fixedNow)
	require.NoError(t, err)
	return tr
}

func TestNewTransfer(t *testing.T) {
	t.Parallel()

	t.Run("creates a PENDING transfer with the given fields", func(t *testing.T) {
		t.Parallel()
		tr := newPending(t)

		assert.Equal(t, "key-1", tr.IdempotencyKey)
		assert.Equal(t, "wallet_1", tr.FromWalletID)
		assert.Equal(t, "wallet_2", tr.ToWalletID)
		assert.Equal(t, int64(100), tr.Amount)
		assert.Equal(t, StatusPending, tr.Status)
		assert.Equal(t, FailureReason(""), tr.FailureReason)
	})

	t.Run("id is a UUIDv7", func(t *testing.T) {
		t.Parallel()
		tr := newPending(t)
		assert.Equal(t, uuid.Version(7), tr.ID.Version())
	})

	t.Run("ids are unique", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t, newPending(t).ID, newPending(t).ID)
	})

	t.Run("createdAt is UTC and truncated to microseconds to match DB precision", func(t *testing.T) {
		t.Parallel()
		tr := newPending(t)
		assert.Equal(t, time.UTC, tr.CreatedAt.Location())
		assert.Equal(t, 123456000, tr.CreatedAt.Nanosecond())
		assert.True(t, tr.CreatedAt.Equal(fixedNow.Truncate(time.Microsecond)))
	})

	invalid := []struct {
		name          string
		key, from, to string
		amount        int64
	}{
		{"empty key", "", "a", "b", 1},
		{"empty from", "k", "", "b", 1},
		{"empty to", "k", "a", "", 1},
		{"same wallet", "k", "a", "a", 1},
		{"zero amount", "k", "a", "b", 0},
		{"negative amount", "k", "a", "b", -1},
	}
	for _, tc := range invalid {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			tr, err := NewTransfer(tc.key, tc.from, tc.to, tc.amount, fixedNow)
			require.ErrorIs(t, err, ErrValidation)
			assert.Nil(t, tr)
		})
	}
}

// TestTransfer_TransitionMatrix covers every (from state, action) pair.
func TestTransfer_TransitionMatrix(t *testing.T) {
	t.Parallel()

	type action struct {
		name       string
		apply      func(*Transfer) error
		wantStatus Status
		wantReason FailureReason
	}
	markProcessed := action{"MarkProcessed", (*Transfer).MarkProcessed, StatusProcessed, ""}
	markFailed := action{"MarkFailed", func(tr *Transfer) error {
		return tr.MarkFailed(ReasonInsufficientFunds)
	}, StatusFailed, ReasonInsufficientFunds}

	// Put a transfer into a given starting state via the public API.
	setup := map[Status]func(*testing.T) *Transfer{
		StatusPending: newPending,
		StatusProcessed: func(t *testing.T) *Transfer {
			tr := newPending(t)
			require.NoError(t, tr.MarkProcessed())
			return tr
		},
		StatusFailed: func(t *testing.T) *Transfer {
			tr := newPending(t)
			require.NoError(t, tr.MarkFailed(ReasonCurrencyMismatch))
			return tr
		},
	}

	tests := []struct {
		from    Status
		action  action
		allowed bool
	}{
		{StatusPending, markProcessed, true},
		{StatusPending, markFailed, true},
		{StatusProcessed, markProcessed, false},
		{StatusProcessed, markFailed, false},
		{StatusFailed, markProcessed, false},
		{StatusFailed, markFailed, false},
	}

	for _, tc := range tests {
		t.Run(string(tc.from)+"->"+tc.action.name, func(t *testing.T) {
			t.Parallel()
			tr := setup[tc.from](t)
			before := *tr

			err := tc.action.apply(tr)

			if tc.allowed {
				require.NoError(t, err)
				assert.Equal(t, tc.action.wantStatus, tr.Status)
				assert.Equal(t, tc.action.wantReason, tr.FailureReason)
				return
			}
			require.ErrorIs(t, err, ErrInvalidTransition)
			assert.Equal(t, before, *tr, "state must be unchanged on a rejected transition")
		})
	}
}

func TestTransfer_MarkFailed_RequiresKnownReason(t *testing.T) {
	t.Parallel()

	for _, reason := range []FailureReason{"", "SOMETHING_ELSE", "insufficient_funds"} {
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()
			tr := newPending(t)
			before := *tr

			require.ErrorIs(t, tr.MarkFailed(reason), ErrValidation)
			assert.Equal(t, before, *tr)
		})
	}
}

func TestTransfer_MarkFailed_AcceptsEveryKnownReason(t *testing.T) {
	t.Parallel()

	for _, reason := range []FailureReason{ReasonInsufficientFunds, ReasonCurrencyMismatch, ReasonBalanceOverflow} {
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()
			tr := newPending(t)
			require.NoError(t, tr.MarkFailed(reason))
			assert.Equal(t, StatusFailed, tr.Status)
			assert.Equal(t, reason, tr.FailureReason)
		})
	}
}
