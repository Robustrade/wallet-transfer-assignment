package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
)

func TestWalletCanDebit(t *testing.T) {
	tests := []struct {
		name    string
		balance int64
		amount  int64
		want    bool
	}{
		{name: "balance exceeds amount", balance: 500, amount: 100, want: true},
		{name: "balance equals amount: may drain to zero", balance: 100, amount: 100, want: true},
		{name: "positive balance but less than amount", balance: 10, amount: 100, want: false},
		{name: "empty wallet", balance: 0, amount: 1, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := domain.Wallet{ID: "wallet_1", Balance: tc.balance}

			assert.Equal(t, tc.want, w.CanDebit(tc.amount))
		})
	}
}
