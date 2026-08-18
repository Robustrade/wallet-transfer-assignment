package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWallet_Debit_Success(t *testing.T) {
	w := &Wallet{Balance: 100}
	err := w.Debit(40)
	require.NoError(t, err)
	require.EqualValues(t, 60, w.Balance)
}

func TestWallet_Debit_ExactBalance(t *testing.T) {
	w := &Wallet{Balance: 50}
	err := w.Debit(50)
	require.NoError(t, err)
	require.EqualValues(t, 0, w.Balance)
}

func TestWallet_Debit_InsufficientBalance(t *testing.T) {
	w := &Wallet{Balance: 10}
	err := w.Debit(20)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.EqualValues(t, 10, w.Balance, "balance must be unchanged on failure")
}

func TestWallet_Debit_NonPositiveAmount(t *testing.T) {
	for _, amount := range []int64{0, -5} {
		w := &Wallet{Balance: 100}
		err := w.Debit(amount)
		require.Error(t, err)
		require.EqualValues(t, 100, w.Balance, "balance must be unchanged on failure")
	}
}

func TestWallet_Credit_Success(t *testing.T) {
	w := &Wallet{Balance: 100}
	err := w.Credit(40)
	require.NoError(t, err)
	require.EqualValues(t, 140, w.Balance)
}

func TestWallet_Credit_NonPositiveAmount(t *testing.T) {
	for _, amount := range []int64{0, -5} {
		w := &Wallet{Balance: 100}
		err := w.Credit(amount)
		require.Error(t, err)
		require.EqualValues(t, 100, w.Balance, "balance must be unchanged on failure")
	}
}
