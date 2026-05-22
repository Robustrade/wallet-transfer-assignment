package domain

import "testing"

func TestWallet_CanDebit(t *testing.T) {
	cases := []struct {
		name    string
		balance int64
		amount  int64
		want    bool
	}{
		{"more than enough", 100, 30, true},
		{"exact balance", 100, 100, true},
		{"one over balance", 100, 101, false},
		{"zero balance", 0, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := Wallet{Balance: tc.balance}
			if got := w.CanDebit(tc.amount); got != tc.want {
				t.Errorf("balance=%d amount=%d: got %v, want %v", tc.balance, tc.amount, got, tc.want)
			}
		})
	}
}
