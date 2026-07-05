package domain_test

import (
	"testing"

	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
)

func TestWallet_HasSufficientFunds(t *testing.T) {
	cases := []struct {
		name    string
		balance int64
		amount  int64
		want    bool
	}{
		{name: "balance greater than amount", balance: 100, amount: 50, want: true},
		{name: "balance equal to amount", balance: 100, amount: 100, want: true},
		{name: "balance less than amount", balance: 50, amount: 100, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wallet := domain.Wallet{Balance: tc.balance}
			got := wallet.HasSufficientFunds(tc.amount)
			if got != tc.want {
				t.Errorf("Expected %v, got %v", tc.want, got)
			}
		})
	}
}
