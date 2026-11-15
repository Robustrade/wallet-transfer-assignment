package domain_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

func newWallet(t *testing.T, balance int64) *domain.Wallet {
	t.Helper()
	wallet, err := domain.NewWallet("wallet_1", balance)
	if err != nil {
		t.Fatalf("NewWallet: unexpected error: %v", err)
	}
	return wallet
}

func TestNewWallet_Valid(t *testing.T) {
	for _, balance := range []int64{0, 2000} {
		wallet, err := domain.NewWallet("wallet_1", balance)
		if err != nil {
			t.Fatalf("NewWallet(balance=%d): unexpected error: %v", balance, err)
		}
		if wallet.ID != "wallet_1" || wallet.Balance != balance {
			t.Errorf("wallet = %+v, want ID wallet_1 with balance %d", wallet, balance)
		}
	}
}

func TestNewWallet_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		balance   int64
		wantField string
	}{
		{"missing id", "", 100, "walletId"},
		{"whitespace-only id", "   ", 100, "walletId"},
		{"id too long", strings.Repeat("w", 256), 100, "walletId"},
		{"negative initial balance", "wallet_1", -1, "initialBalance"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewWallet(tc.id, tc.balance)

			var validationErr *domain.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %v, want *domain.ValidationError", err)
			}
			if validationErr.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", validationErr.Field, tc.wantField)
			}
		})
	}
}

func TestWallet_DebitReducesBalance(t *testing.T) {
	wallet := newWallet(t, 1000)

	if err := wallet.Debit(300); err != nil {
		t.Fatalf("Debit: unexpected error: %v", err)
	}
	if wallet.Balance != 700 {
		t.Errorf("Balance = %d, want 700", wallet.Balance)
	}
}

func TestWallet_DebitEntireBalance(t *testing.T) {
	wallet := newWallet(t, 1000)

	if err := wallet.Debit(1000); err != nil {
		t.Fatalf("Debit: unexpected error: %v", err)
	}
	if wallet.Balance != 0 {
		t.Errorf("Balance = %d, want 0", wallet.Balance)
	}
}

func TestWallet_DebitMoreThanBalanceFails(t *testing.T) {
	wallet := newWallet(t, 1000)

	err := wallet.Debit(1001)

	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("error = %v, want ErrInsufficientFunds", err)
	}
	if wallet.Balance != 1000 {
		t.Errorf("Balance = %d, want it unchanged at 1000", wallet.Balance)
	}
}

func TestWallet_CreditIncreasesBalance(t *testing.T) {
	wallet := newWallet(t, 1000)

	if err := wallet.Credit(250); err != nil {
		t.Fatalf("Credit: unexpected error: %v", err)
	}
	if wallet.Balance != 1250 {
		t.Errorf("Balance = %d, want 1250", wallet.Balance)
	}
}

func TestWallet_CreditUpToMaximumBalance(t *testing.T) {
	wallet := newWallet(t, math.MaxInt64-1)

	if err := wallet.Credit(1); err != nil {
		t.Fatalf("Credit: unexpected error: %v", err)
	}
	if wallet.Balance != math.MaxInt64 {
		t.Errorf("Balance = %d, want %d", wallet.Balance, int64(math.MaxInt64))
	}
}

func TestWallet_CreditRejectsOverflow(t *testing.T) {
	wallet := newWallet(t, math.MaxInt64)

	err := wallet.Credit(1)

	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want *domain.ValidationError", err)
	}
	if wallet.Balance != math.MaxInt64 {
		t.Errorf("Balance = %d, want it unchanged at %d", wallet.Balance, int64(math.MaxInt64))
	}
}

func TestWallet_RejectsNonPositiveAmounts(t *testing.T) {
	operations := map[string]func(w *domain.Wallet, amount int64) error{
		"debit":  (*domain.Wallet).Debit,
		"credit": (*domain.Wallet).Credit,
	}

	for name, operation := range operations {
		for _, amount := range []int64{0, -50} {
			wallet := newWallet(t, 1000)

			err := operation(wallet, amount)

			var validationErr *domain.ValidationError
			if !errors.As(err, &validationErr) {
				t.Errorf("%s(%d): error = %v, want *domain.ValidationError", name, amount, err)
			}
			if wallet.Balance != 1000 {
				t.Errorf("%s(%d): Balance = %d, want it unchanged at 1000", name, amount, wallet.Balance)
			}
		}
	}
}
