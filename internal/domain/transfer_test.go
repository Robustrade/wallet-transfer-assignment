package domain_test

import (
	"testing"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTransfer(t *testing.T) {
	t.Parallel()

	t.Run("creates a PENDING transfer for a valid request", func(t *testing.T) {
		t.Parallel()
		tr, err := domain.NewTransfer("id1", "key1", "w1", "w2", 100)

		require.NoError(t, err)
		assert.Equal(t, domain.StatusPending, tr.Status)
		assert.Equal(t, domain.Money(100), tr.Amount)
		assert.False(t, tr.CreatedAt.IsZero())
	})

	t.Run("rejects invalid requests", func(t *testing.T) {
		t.Parallel()
		cases := map[string]struct {
			key, from, to string
			amount        domain.Money
			wantErr       error
		}{
			"missing idempotency key": {"", "w1", "w2", 100, domain.ErrMissingIdempotencyKey},
			"missing source wallet":   {"k", "", "w2", 100, domain.ErrMissingWalletID},
			"missing target wallet":   {"k", "w1", "", 100, domain.ErrMissingWalletID},
			"same wallet":             {"k", "w1", "w1", 100, domain.ErrSameWallet},
			"zero amount":             {"k", "w1", "w2", 0, domain.ErrInvalidAmount},
			"negative amount":         {"k", "w1", "w2", -1, domain.ErrInvalidAmount},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := domain.NewTransfer("id", tc.key, tc.from, tc.to, tc.amount)
				assert.ErrorIs(t, err, tc.wantErr)
			})
		}
	})
}

func TestTransferStateMachine(t *testing.T) {
	t.Parallel()

	t.Run("PENDING can transition to PROCESSED", func(t *testing.T) {
		t.Parallel()
		tr := mustTransfer(t)
		require.NoError(t, tr.MarkProcessed())
		assert.Equal(t, domain.StatusProcessed, tr.Status)
	})

	t.Run("PENDING can transition to FAILED with a reason", func(t *testing.T) {
		t.Parallel()
		tr := mustTransfer(t)
		require.NoError(t, tr.MarkFailed("insufficient funds"))
		assert.Equal(t, domain.StatusFailed, tr.Status)
		assert.Equal(t, "insufficient funds", tr.FailureReason)
	})

	t.Run("terminal states reject further transitions", func(t *testing.T) {
		t.Parallel()
		processed := mustTransfer(t)
		require.NoError(t, processed.MarkProcessed())
		assert.ErrorIs(t, processed.MarkProcessed(), domain.ErrInvalidStateTransition)
		assert.ErrorIs(t, processed.MarkFailed("x"), domain.ErrInvalidStateTransition)

		failed := mustTransfer(t)
		require.NoError(t, failed.MarkFailed("x"))
		assert.ErrorIs(t, failed.MarkProcessed(), domain.ErrInvalidStateTransition)
	})
}

func TestNewDoubleEntry(t *testing.T) {
	t.Parallel()

	entries := domain.NewDoubleEntry("d", "c", "t1", "w1", "w2", 100)

	require.Len(t, entries, 2)
	debit, credit := entries[0], entries[1]

	assert.Equal(t, domain.EntryDebit, debit.Type)
	assert.Equal(t, "w1", debit.WalletID)
	assert.Equal(t, domain.EntryCredit, credit.Type)
	assert.Equal(t, "w2", credit.WalletID)
	assert.Equal(t, debit.Amount, credit.Amount, "double-entry must balance")
	assert.Equal(t, "t1", debit.TransferID)
	assert.Equal(t, "t1", credit.TransferID)
}

func mustTransfer(t *testing.T) *domain.Transfer {
	t.Helper()
	tr, err := domain.NewTransfer("id", "key", "w1", "w2", 100)
	require.NoError(t, err)
	return tr
}
