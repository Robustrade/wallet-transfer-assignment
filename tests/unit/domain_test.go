package unit

import (
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Wallet tests ---

func TestWallet_HasSufficientFunds(t *testing.T) {
	tests := []struct {
		name     string
		balance  int64
		amount   int64
		expected bool
	}{
		{name: "sufficient funds", balance: 1000, amount: 500, expected: true},
		{name: "exact balance", balance: 1000, amount: 1000, expected: true},
		{name: "insufficient funds", balance: 500, amount: 1000, expected: false},
		{name: "zero balance", balance: 0, amount: 1, expected: false},
		{name: "zero amount", balance: 1000, amount: 0, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &domain.Wallet{Balance: tt.balance}
			assert.Equal(t, tt.expected, w.HasSufficientFunds(tt.amount))
		})
	}
}

func TestWallet_Debit(t *testing.T) {
	t.Run("successful debit", func(t *testing.T) {
		w := &domain.Wallet{Balance: 1000}
		ok := w.Debit(300)
		assert.True(t, ok)
		assert.Equal(t, int64(700), w.Balance)
	})

	t.Run("insufficient funds debit", func(t *testing.T) {
		w := &domain.Wallet{Balance: 100}
		ok := w.Debit(300)
		assert.False(t, ok)
		assert.Equal(t, int64(100), w.Balance) // Balance unchanged
	})

	t.Run("exact balance debit", func(t *testing.T) {
		w := &domain.Wallet{Balance: 500}
		ok := w.Debit(500)
		assert.True(t, ok)
		assert.Equal(t, int64(0), w.Balance)
	})
}

func TestWallet_Credit(t *testing.T) {
	w := &domain.Wallet{Balance: 500}
	w.Credit(300)
	assert.Equal(t, int64(800), w.Balance)
}

// --- Transfer tests ---

func TestNewTransfer_Validation(t *testing.T) {
	tests := []struct {
		name           string
		idempotencyKey string
		fromWallet     string
		toWallet       string
		amount         int64
		expectedErr    error
	}{
		{
			name:           "valid transfer",
			idempotencyKey: "key1",
			fromWallet:     "wallet_1",
			toWallet:       "wallet_2",
			amount:         100,
			expectedErr:    nil,
		},
		{
			name:           "empty idempotency key",
			idempotencyKey: "",
			fromWallet:     "wallet_1",
			toWallet:       "wallet_2",
			amount:         100,
			expectedErr:    domain.ErrEmptyIdempotencyKey,
		},
		{
			name:           "empty from wallet",
			idempotencyKey: "key1",
			fromWallet:     "",
			toWallet:       "wallet_2",
			amount:         100,
			expectedErr:    domain.ErrEmptyWalletID,
		},
		{
			name:           "same wallet",
			idempotencyKey: "key1",
			fromWallet:     "wallet_1",
			toWallet:       "wallet_1",
			amount:         100,
			expectedErr:    domain.ErrSameWallet,
		},
		{
			name:           "zero amount",
			idempotencyKey: "key1",
			fromWallet:     "wallet_1",
			toWallet:       "wallet_2",
			amount:         0,
			expectedErr:    domain.ErrInvalidAmount,
		},
		{
			name:           "negative amount",
			idempotencyKey: "key1",
			fromWallet:     "wallet_1",
			toWallet:       "wallet_2",
			amount:         -100,
			expectedErr:    domain.ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer, err := domain.NewTransfer(tt.idempotencyKey, tt.fromWallet, tt.toWallet, tt.amount)
			if tt.expectedErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.expectedErr)
				assert.Nil(t, transfer)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, transfer)
				assert.Equal(t, domain.TransferStatusPending, transfer.Status)
			}
		})
	}
}

func TestTransfer_StateTransitions(t *testing.T) {
	t.Run("PENDING to PROCESSED", func(t *testing.T) {
		transfer, err := domain.NewTransfer("key1", "w1", "w2", 100)
		require.NoError(t, err)

		err = transfer.MarkProcessed()
		require.NoError(t, err)
		assert.Equal(t, domain.TransferStatusProcessed, transfer.Status)
	})

	t.Run("PENDING to FAILED", func(t *testing.T) {
		transfer, err := domain.NewTransfer("key1", "w1", "w2", 100)
		require.NoError(t, err)

		err = transfer.MarkFailed("insufficient funds")
		require.NoError(t, err)
		assert.Equal(t, domain.TransferStatusFailed, transfer.Status)
		assert.Equal(t, "insufficient funds", transfer.FailureReason)
	})

	t.Run("PROCESSED to PROCESSED is invalid", func(t *testing.T) {
		transfer, err := domain.NewTransfer("key1", "w1", "w2", 100)
		require.NoError(t, err)

		err = transfer.MarkProcessed()
		require.NoError(t, err)

		err = transfer.MarkProcessed()
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidStateTransition)
	})

	t.Run("FAILED to PROCESSED is invalid", func(t *testing.T) {
		transfer, err := domain.NewTransfer("key1", "w1", "w2", 100)
		require.NoError(t, err)

		err = transfer.MarkFailed("some reason")
		require.NoError(t, err)

		err = transfer.MarkProcessed()
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidStateTransition)
	})

	t.Run("PROCESSED to FAILED is invalid", func(t *testing.T) {
		transfer, err := domain.NewTransfer("key1", "w1", "w2", 100)
		require.NoError(t, err)

		err = transfer.MarkProcessed()
		require.NoError(t, err)

		err = transfer.MarkFailed("some reason")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidStateTransition)
	})
}

func TestTransfer_IsTerminal(t *testing.T) {
	t.Run("PENDING is not terminal", func(t *testing.T) {
		transfer, _ := domain.NewTransfer("key1", "w1", "w2", 100)
		assert.False(t, transfer.IsTerminal())
	})

	t.Run("PROCESSED is terminal", func(t *testing.T) {
		transfer, _ := domain.NewTransfer("key1", "w1", "w2", 100)
		_ = transfer.MarkProcessed()
		assert.True(t, transfer.IsTerminal())
	})

	t.Run("FAILED is terminal", func(t *testing.T) {
		transfer, _ := domain.NewTransfer("key1", "w1", "w2", 100)
		_ = transfer.MarkFailed("reason")
		assert.True(t, transfer.IsTerminal())
	})
}

// --- LedgerEntry tests ---

func TestNewDebitEntry(t *testing.T) {
	entry := domain.NewDebitEntry("transfer_1", "wallet_1", 500)
	assert.Equal(t, "transfer_1", entry.TransferID)
	assert.Equal(t, "wallet_1", entry.WalletID)
	assert.Equal(t, domain.EntryTypeDebit, entry.EntryType)
	assert.Equal(t, int64(500), entry.Amount)
}

func TestNewCreditEntry(t *testing.T) {
	entry := domain.NewCreditEntry("transfer_1", "wallet_2", 500)
	assert.Equal(t, "transfer_1", entry.TransferID)
	assert.Equal(t, "wallet_2", entry.WalletID)
	assert.Equal(t, domain.EntryTypeCredit, entry.EntryType)
	assert.Equal(t, int64(500), entry.Amount)
}
