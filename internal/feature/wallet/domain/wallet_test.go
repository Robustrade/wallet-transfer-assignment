package domain_test

import (
	"errors"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
)

func TestWalletCanDebit(t *testing.T) {
	tests := []struct {
		name    string
		balance int64
		amount  int64
		wantErr error
	}{
		{"enough balance", 100, 40, nil},
		{"exact balance", 100, 100, nil},
		{"one short", 99, 100, domain.ErrInsufficientFunds},
		{"empty wallet", 0, 1, domain.ErrInsufficientFunds},
		{"zero amount", 100, 0, domain.ErrInvalidAmount},
		{"negative amount", 100, -1, domain.ErrInvalidAmount},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.Wallet{ID: 1, Balance: tc.balance}.CanDebit(tc.amount)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}
