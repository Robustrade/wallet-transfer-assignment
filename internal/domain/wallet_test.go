package domain_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/domain"
)

func TestWallet_Debit(t *testing.T) {
	t.Parallel()

	w := domain.Wallet{
		ID:       "wallet_1",
		Currency: "INR",
		Balance:  1000,
	}

	newBalance, err := w.Debit(400)
	require.NoError(t, err)
	require.Equal(t, domain.Amount(600), newBalance)

	_, err = w.Debit(1500)
	require.ErrorIs(t, err, domain.ErrInsufficientFunds)

	_, err = w.Debit(0)
	require.ErrorIs(t, err, domain.ErrValidation)

	_, err = w.Debit(-100)
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestWallet_Credit(t *testing.T) {
	t.Parallel()

	w := domain.Wallet{
		ID:       "wallet_1",
		Currency: "INR",
		Balance:  1000,
	}

	newBalance, err := w.Credit(500)
	require.NoError(t, err)
	require.Equal(t, domain.Amount(1500), newBalance)

	_, err = w.Credit(0)
	require.ErrorIs(t, err, domain.ErrValidation)

	wMax := domain.Wallet{
		ID:       "wallet_max",
		Currency: "INR",
		Balance:  domain.Amount(math.MaxInt64),
	}
	_, err = wMax.Credit(1)
	require.ErrorIs(t, err, domain.ErrValidation)
}
