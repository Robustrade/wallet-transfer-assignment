package domain_test

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/domain"
)

func TestValidateAmount(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.ValidateAmount(1))
	require.NoError(t, domain.ValidateAmount(100))
	require.NoError(t, domain.ValidateAmount(domain.MaxAmount))

	require.ErrorIs(t, domain.ValidateAmount(0), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateAmount(-1), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateAmount(domain.MaxAmount+1), domain.ErrValidation)
}

func TestCanAdd(t *testing.T) {
	t.Parallel()

	require.True(t, domain.CanAdd(100, 50))
	require.True(t, domain.CanAdd(100, -50))
	require.True(t, domain.CanAdd(math.MaxInt64-1, 1))
	require.False(t, domain.CanAdd(math.MaxInt64, 1))
	require.False(t, domain.CanAdd(math.MinInt64, -1))
}

func TestValidateWalletID(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.ValidateWalletID("fromWalletId", "wallet_1"))
	require.ErrorIs(t, domain.ValidateWalletID("fromWalletId", ""), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateWalletID("fromWalletId", strings.Repeat("a", 65)), domain.ErrValidation)
}

func TestValidateIdempotencyKey(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.ValidateIdempotencyKey("idem-123"))
	require.ErrorIs(t, domain.ValidateIdempotencyKey(""), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateIdempotencyKey("   "), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateIdempotencyKey(strings.Repeat("k", 256)), domain.ErrValidation)
}

func TestValidateCurrency(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.ValidateCurrency("INR"))
	require.NoError(t, domain.ValidateCurrency("USD"))

	require.ErrorIs(t, domain.ValidateCurrency(""), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateCurrency("inr"), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateCurrency("INRR"), domain.ErrValidation)
	require.ErrorIs(t, domain.ValidateCurrency("123"), domain.ErrValidation)
}
