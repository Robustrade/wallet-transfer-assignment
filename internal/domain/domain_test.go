package domain

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func money(v string) decimal.Decimal { return decimal.RequireFromString(v) }

func TestNewTransferParams_Validate(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	tests := []struct {
		name    string
		params  NewTransferParams
		wantErr error
	}{
		{"valid", NewTransferParams{"key", from, to, money("100")}, nil},
		{"max length key", NewTransferParams{strings.Repeat("k", MaxIdempotencyKeyLength), from, to, money("100")}, nil},
		{"empty key", NewTransferParams{"", from, to, money("100")}, ErrEmptyIdempotencyKey},
		{"too long key", NewTransferParams{strings.Repeat("k", MaxIdempotencyKeyLength+1), from, to, money("100")}, ErrIdempotencyKeyTooLong},
		{"zero amount", NewTransferParams{"key", from, to, money("0")}, ErrInvalidAmount},
		{"negative amount", NewTransferParams{"key", from, to, money("-5")}, ErrInvalidAmount},
		{"same wallet", NewTransferParams{"key", from, from, money("100")}, ErrSameWallet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.params.Validate(); got != tt.wantErr {
				t.Fatalf("got %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestWallet_CanDebit(t *testing.T) {
	tests := []struct {
		name    string
		wallet  Wallet
		amount  decimal.Decimal
		wantErr error
	}{
		{"sufficient", Wallet{Balance: money("100"), IsActive: true}, money("80"), nil},
		{"exact", Wallet{Balance: money("100"), IsActive: true}, money("100"), nil},
		{"insufficient", Wallet{Balance: money("50"), IsActive: true}, money("80"), ErrInsufficientFunds},
		{"inactive", Wallet{Balance: money("100"), IsActive: false}, money("10"), ErrWalletInactive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.wallet.CanDebit(tt.amount); got != tt.wantErr {
				t.Fatalf("got %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestTransferState_CanTransitionTo(t *testing.T) {
	allowed := []struct{ from, to TransferState }{
		{StatePending, StateProcessing},
		{StateProcessing, StateProcessed},
		{StateProcessing, StateFailed},
		{StateProcessing, StatePending},
	}
	for _, tc := range allowed {
		if !tc.from.CanTransitionTo(tc.to) {
			t.Errorf("expected %s -> %s allowed", tc.from, tc.to)
		}
	}

	forbidden := []struct{ from, to TransferState }{
		{StatePending, StateProcessed},     // must go through PROCESSING
		{StateProcessed, StateProcessing},  // terminal cannot reopen
		{StateFailed, StateProcessing},     // terminal cannot reopen
		{StateProcessing, StateProcessing}, // no self-loop
	}
	for _, tc := range forbidden {
		if tc.from.CanTransitionTo(tc.to) {
			t.Errorf("expected %s -> %s forbidden", tc.from, tc.to)
		}
	}
}
