package domain_test

import (
	"testing"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

func TestCanCover(t *testing.T) {
	tests := map[string]struct {
		balance int64
		amount  int64
		want    bool
	}{
		"balance exceeds the amount": {balance: 100, amount: 40, want: true},
		"balance exactly covers it":  {balance: 100, amount: 100, want: true},
		"balance falls one short":    {balance: 99, amount: 100, want: false},
		"empty wallet":               {balance: 0, amount: 1, want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			wallet := domain.Wallet{ID: "wallet_1", Balance: tc.balance}

			if got := wallet.CanCover(tc.amount); got != tc.want {
				t.Fatalf("wallet holding %d covering %d = %v, want %v",
					tc.balance, tc.amount, got, tc.want)
			}
		})
	}
}
