package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
)

func TestLedgerTypeValid(t *testing.T) {
	if !domain.LedgerDebit.Valid() || !domain.LedgerCredit.Valid() {
		t.Error("DEBIT and CREDIT must be valid")
	}
	if domain.LedgerType("TRANSFER").Valid() {
		t.Error("TRANSFER must be invalid")
	}
}

func TestWalletHasSufficientFunds(t *testing.T) {
	w := domain.Wallet{Balance: decimal.RequireFromString("100.00")}
	tests := []struct {
		amount string
		want   bool
	}{
		{"99.99", true},
		{"100.00", true},
		{"100.01", false},
	}
	for _, tt := range tests {
		if got := w.HasSufficientFunds(decimal.RequireFromString(tt.amount)); got != tt.want {
			t.Errorf("balance 100.00 vs %s: got %v, want %v", tt.amount, got, tt.want)
		}
	}
}
