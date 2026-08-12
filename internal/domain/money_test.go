package domain_test

import (
	"testing"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestValidAmount(t *testing.T) {
	cases := []struct {
		name   string
		amount int64
		want   bool
	}{
		{"zero", 0, false},
		{"negative", -1, false},
		{"one minor unit", 1, true},
		{"typical", 100, true},
		{"at max", domain.MaxAmountMinor, true},
		{"above max", domain.MaxAmountMinor + 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, domain.ValidAmount(tc.amount))
		})
	}
}

func TestValidCurrency(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"INR", true},
		{"USD", true},
		{"inr", false},
		{"IN", false},
		{"INRX", false},
		{"", false},
		{"123", false},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			require.Equal(t, tc.want, domain.ValidCurrency(tc.code))
		})
	}
}
