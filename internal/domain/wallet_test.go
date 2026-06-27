package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
)

func TestWalletDebit(t *testing.T) {
	t.Parallel()

	t.Run("debits a sufficient balance", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: 100}

		require.NoError(t, w.Debit(40))
		assert.Equal(t, domain.Money(60), w.Balance)
	})

	t.Run("rejects an overdraft and leaves the balance untouched", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: 30}

		err := w.Debit(40)

		assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
		assert.Equal(t, domain.Money(30), w.Balance, "balance must not change on a failed debit")
	})

	t.Run("rejects a non-positive amount", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: 30}

		assert.ErrorIs(t, w.Debit(0), domain.ErrInvalidAmount)
		assert.ErrorIs(t, w.Debit(-5), domain.ErrInvalidAmount)
		assert.Equal(t, domain.Money(30), w.Balance)
	})
}

func TestWalletCredit(t *testing.T) {
	t.Parallel()

	t.Run("credits a positive amount", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: 100}

		require.NoError(t, w.Credit(25))
		assert.Equal(t, domain.Money(125), w.Balance)
	})

	t.Run("rejects a non-positive amount", func(t *testing.T) {
		t.Parallel()
		w := &domain.Wallet{ID: "w1", Balance: 100}

		assert.ErrorIs(t, w.Credit(0), domain.ErrInvalidAmount)
		assert.Equal(t, domain.Money(100), w.Balance)
	})
}
