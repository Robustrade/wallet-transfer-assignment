package domain_test

import (
	"errors"
	"testing"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

func usd(n int64) domain.Money { return domain.Money{Amount: n, Currency: "USD"} }

func TestNewMoney(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		amount   int64
		currency string
		wantErr  error
	}{
		{"valid", 100, "USD", nil},
		{"zero is valid", 0, "USD", nil},
		{"negative rejected", -1, "USD", domain.ErrValidation},
		{"empty currency rejected", 100, "", domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewMoney(tt.amount, tt.currency)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("got error %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestMoneySub(t *testing.T) {
	t.Parallel()

	t.Run("normal subtraction", func(t *testing.T) {
		t.Parallel()
		got, err := usd(100).Sub(usd(30))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Amount != 70 {
			t.Fatalf("got %d, want 70", got.Amount)
		}
	})

	t.Run("subtracting more than balance is insufficient funds", func(t *testing.T) {
		t.Parallel()
		_, err := usd(100).Sub(usd(101))
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			t.Fatalf("got %v, want ErrInsufficientFunds", err)
		}
	})

	t.Run("currency mismatch", func(t *testing.T) {
		t.Parallel()
		_, err := usd(100).Sub(domain.Money{Amount: 10, Currency: "EUR"})
		if !errors.Is(err, domain.ErrCurrencyMismatch) {
			t.Fatalf("got %v, want ErrCurrencyMismatch", err)
		}
	})
}

func TestMoneyAdd(t *testing.T) {
	t.Parallel()
	got, err := usd(100).Add(usd(50))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount != 150 {
		t.Fatalf("got %d, want 150", got.Amount)
	}
}
