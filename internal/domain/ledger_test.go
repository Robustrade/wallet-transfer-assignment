package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestNewLedgerEntries(t *testing.T) {
	t.Parallel()

	tr := newPending(t)

	debit, credit := NewLedgerEntries(tr)

	t.Run("debit is on the source wallet", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, EntryDebit, debit.Type)
		assert.Equal(t, tr.FromWalletID, debit.WalletID)
	})

	t.Run("credit is on the destination wallet", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, EntryCredit, credit.Type)
		assert.Equal(t, tr.ToWalletID, credit.WalletID)
	})

	t.Run("both entries carry the transfer id, amount and time", func(t *testing.T) {
		t.Parallel()
		for _, e := range []LedgerEntry{debit, credit} {
			assert.Equal(t, tr.ID, e.TransferID)
			assert.Equal(t, tr.Amount, e.Amount)
			assert.True(t, e.CreatedAt.Equal(tr.CreatedAt))
		}
	})

	t.Run("entries balance: debits equal credits", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, debit.Amount, credit.Amount)
	})

	t.Run("each entry has its own UUIDv7 id", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, uuid.Version(7), debit.ID.Version())
		assert.Equal(t, uuid.Version(7), credit.ID.Version())
		assert.NotEqual(t, debit.ID, credit.ID)
	})
}
