package domain_test

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
)

func TestValidateTransferInput(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		from    string
		to      string
		amount  decimal.Decimal
		wantErr error
	}{
		{"valid", "k1", "w1", "w2", decimal.NewFromInt(100), nil},
		{"missing key", "", "w1", "w2", decimal.NewFromInt(100), domain.ErrMissingField},
		{"missing from", "k1", "", "w2", decimal.NewFromInt(100), domain.ErrMissingField},
		{"missing to", "k1", "w1", "", decimal.NewFromInt(100), domain.ErrMissingField},
		{"same wallet", "k1", "w1", "w1", decimal.NewFromInt(100), domain.ErrSameWallet},
		{"zero amount", "k1", "w1", "w2", decimal.Zero, domain.ErrInvalidAmount},
		{"negative amount", "k1", "w1", "w2", decimal.NewFromInt(-1), domain.ErrInvalidAmount},
		{"fractional ok", "k1", "w1", "w2", decimal.RequireFromString("0.01"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateTransferInput(tt.key, tt.from, tt.to, tt.amount)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestTransferStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		from domain.TransferStatus
		to   domain.TransferStatus
		want bool
	}{
		{domain.TransferPending, domain.TransferProcessed, true},
		{domain.TransferPending, domain.TransferFailed, true},
		{domain.TransferPending, domain.TransferPending, false},
		{domain.TransferProcessed, domain.TransferFailed, false},
		{domain.TransferProcessed, domain.TransferProcessed, false},
		{domain.TransferFailed, domain.TransferProcessed, false},
		{domain.TransferFailed, domain.TransferPending, false},
	}
	for _, tt := range tests {
		if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
			t.Errorf("%s -> %s: got %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestTransferStatusIsTerminal(t *testing.T) {
	cases := map[domain.TransferStatus]bool{
		domain.TransferPending:   false,
		domain.TransferProcessed: true,
		domain.TransferFailed:    true,
	}
	for s, want := range cases {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s IsTerminal: got %v, want %v", s, got, want)
		}
	}
}

func TestTransferStatusValid(t *testing.T) {
	for _, s := range []domain.TransferStatus{domain.TransferPending, domain.TransferProcessed, domain.TransferFailed} {
		if !s.Valid() {
			t.Errorf("%s should be valid", s)
		}
	}
	if domain.TransferStatus("BOGUS").Valid() {
		t.Error("BOGUS should be invalid")
	}
}
