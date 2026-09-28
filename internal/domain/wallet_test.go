package domain

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWallet_Debit(t *testing.T) {
	t.Parallel()

	t.Run("partial amount reduces balance", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: 100, Currency: "SGD"}
		require.NoError(t, w.Debit(30))
		assert.Equal(t, int64(70), w.Balance)
	})

	t.Run("exact balance leaves zero", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: 100, Currency: "SGD"}
		require.NoError(t, w.Debit(100))
		assert.Equal(t, int64(0), w.Balance)
	})

	t.Run("balance plus one is insufficient and leaves balance unchanged", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: 100, Currency: "SGD"}
		err := w.Debit(101)
		require.ErrorIs(t, err, ErrInsufficientFunds)
		assert.Equal(t, int64(100), w.Balance)
	})

	t.Run("debit from empty wallet is insufficient", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: 0, Currency: "SGD"}
		require.ErrorIs(t, w.Debit(1), ErrInsufficientFunds)
		assert.Equal(t, int64(0), w.Balance)
	})

	for _, amount := range []int64{0, -1, math.MinInt64} {
		t.Run("non-positive amount is rejected", func(t *testing.T) {
			t.Parallel()
			w := Wallet{ID: "w1", Balance: 100, Currency: "SGD"}
			require.ErrorIs(t, w.Debit(amount), ErrValidation)
			assert.Equal(t, int64(100), w.Balance)
		})
	}
}

func TestWallet_Credit(t *testing.T) {
	t.Parallel()

	t.Run("adds to balance", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: 100, Currency: "SGD"}
		require.NoError(t, w.Credit(50))
		assert.Equal(t, int64(150), w.Balance)
	})

	t.Run("reaching exactly MaxInt64 is allowed", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: math.MaxInt64 - 10, Currency: "SGD"}
		require.NoError(t, w.Credit(10))
		assert.Equal(t, int64(math.MaxInt64), w.Balance)
	})

	t.Run("overflow near MaxInt64 is rejected and leaves balance unchanged", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: math.MaxInt64 - 10, Currency: "SGD"}
		err := w.Credit(11)
		require.ErrorIs(t, err, ErrBalanceOverflow)
		assert.Equal(t, int64(math.MaxInt64-10), w.Balance)
	})

	t.Run("overflow at MaxInt64", func(t *testing.T) {
		t.Parallel()
		w := Wallet{ID: "w1", Balance: math.MaxInt64, Currency: "SGD"}
		require.ErrorIs(t, w.Credit(1), ErrBalanceOverflow)
		assert.Equal(t, int64(math.MaxInt64), w.Balance)
	})

	for _, amount := range []int64{0, -1, math.MinInt64} {
		t.Run("non-positive amount is rejected", func(t *testing.T) {
			t.Parallel()
			w := Wallet{ID: "w1", Balance: 100, Currency: "SGD"}
			require.ErrorIs(t, w.Credit(amount), ErrValidation)
			assert.Equal(t, int64(100), w.Balance)
		})
	}
}

func TestApplyTransfer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		from, to    Wallet
		amount      int64
		wantErr     error
		wantFromBal int64
		wantToBal   int64
	}{
		{
			name:        "moves amount from source to destination",
			from:        Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: 5, Currency: "SGD"},
			amount:      40,
			wantFromBal: 60,
			wantToBal:   45,
		},
		{
			name:        "full balance leaves source at zero",
			from:        Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: 0, Currency: "SGD"},
			amount:      100,
			wantFromBal: 0,
			wantToBal:   100,
		},
		{
			name:        "currency mismatch changes nothing",
			from:        Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: 0, Currency: "USD"},
			amount:      10,
			wantErr:     ErrCurrencyMismatch,
			wantFromBal: 100,
			wantToBal:   0,
		},
		{
			name:        "insufficient funds changes nothing",
			from:        Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: 0, Currency: "SGD"},
			amount:      101,
			wantErr:     ErrInsufficientFunds,
			wantFromBal: 100,
			wantToBal:   0,
		},
		{
			// The source could pay, but the destination would overflow. The
			// source must NOT be left debited in memory.
			name:        "destination overflow leaves source untouched",
			from:        Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: math.MaxInt64 - 5, Currency: "SGD"},
			amount:      10,
			wantErr:     ErrBalanceOverflow,
			wantFromBal: 100,
			wantToBal:   math.MaxInt64 - 5,
		},
		{
			name:        "currency is checked before funds",
			from:        Wallet{ID: "a", Balance: 0, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: 0, Currency: "USD"},
			amount:      10,
			wantErr:     ErrCurrencyMismatch,
			wantFromBal: 0,
			wantToBal:   0,
		},
		{
			name:        "funds are checked before overflow",
			from:        Wallet{ID: "a", Balance: 5, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: math.MaxInt64, Currency: "SGD"},
			amount:      10,
			wantErr:     ErrInsufficientFunds,
			wantFromBal: 5,
			wantToBal:   math.MaxInt64,
		},
		{
			name:        "non-positive amount is a validation error",
			from:        Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			to:          Wallet{ID: "b", Balance: 0, Currency: "SGD"},
			amount:      0,
			wantErr:     ErrValidation,
			wantFromBal: 100,
			wantToBal:   0,
		},
		{
			name:        "same wallet is a validation error",
			from:        Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			to:          Wallet{ID: "a", Balance: 100, Currency: "SGD"},
			amount:      10,
			wantErr:     ErrValidation,
			wantFromBal: 100,
			wantToBal:   100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			from, to := tc.from, tc.to

			err := ApplyTransfer(&from, &to, tc.amount)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantFromBal, from.Balance, "source balance")
			assert.Equal(t, tc.wantToBal, to.Balance, "destination balance")
		})
	}
}
