package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTransferRequest_Validate(t *testing.T) {
	fromID := uuid.New()
	toID := uuid.New()
	validAmount, err := ParseAmount("100.0000")
	require.NoError(t, err)

	t.Run("valid request", func(t *testing.T) {
		r := CreateTransferRequest{
			IdempotencyKey: "key-1",
			FromWalletID:   fromID,
			ToWalletID:     toID,
			Amount:         validAmount,
		}
		assert.NoError(t, r.Validate())
	})

	t.Run("missing idempotency key", func(t *testing.T) {
		r := CreateTransferRequest{FromWalletID: fromID, ToWalletID: toID, Amount: validAmount}
		assert.ErrorIs(t, r.Validate(), ErrMissingIdempotencyKey)
	})

	t.Run("same wallet", func(t *testing.T) {
		r := CreateTransferRequest{
			IdempotencyKey: "key-1",
			FromWalletID:   fromID,
			ToWalletID:     fromID,
			Amount:         validAmount,
		}
		assert.ErrorIs(t, r.Validate(), ErrSameWallet)
	})

	t.Run("zero amount", func(t *testing.T) {
		zero, _ := ParseAmount("0")
		r := CreateTransferRequest{
			IdempotencyKey: "key-1",
			FromWalletID:   fromID,
			ToWalletID:     toID,
			Amount:         zero,
		}
		assert.ErrorIs(t, r.Validate(), ErrInvalidAmount)
	})
}

func TestParseAmount(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"100", "100.0000", false},
		{"100.0000", "100.0000", false},
		{"0.0001", "0.0001", false},
		{"0", "0.0000", false},
		{"-1", "", true},
		{"abc", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseAmount(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
		})
	}
}

func TestAmount_Arithmetic(t *testing.T) {
	a, _ := ParseAmount("100.0000")
	b, _ := ParseAmount("30.5000")

	assert.Equal(t, "130.5000", a.Add(b).String())
	assert.Equal(t, "69.5000", a.Sub(b).String())
	assert.True(t, a.GTE(b))
	assert.False(t, b.GTE(a))
	assert.True(t, a.GTE(a))
}
