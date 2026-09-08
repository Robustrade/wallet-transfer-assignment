package domain

import (
	"errors"
	"testing"
)

func TestNewTransferRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		req     NewTransferRequest
		wantErr error
	}{
		{
			name:    "valid request",
			req:     NewTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 100},
			wantErr: nil,
		},
		{
			name:    "missing idempotency key",
			req:     NewTransferRequest{FromWalletID: "w1", ToWalletID: "w2", Amount: 100},
			wantErr: ErrIdempotencyKeyEmpty,
		},
		{
			name:    "same wallet",
			req:     NewTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w1", Amount: 100},
			wantErr: ErrSameWallet,
		},
		{
			name:    "zero amount",
			req:     NewTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 0},
			wantErr: ErrInvalidAmount,
		},
		{
			name:    "negative amount",
			req:     NewTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: -50},
			wantErr: ErrInvalidAmount,
		},
		{
			name:    "empty from wallet",
			req:     NewTransferRequest{IdempotencyKey: "k1", FromWalletID: "", ToWalletID: "w2", Amount: 100},
			wantErr: ErrWalletIdEmpty,
		},
		{
			name:    "empty to wallet",
			req:     NewTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "", Amount: 100},
			wantErr: ErrWalletIdEmpty,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestTransferStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from TransferStatus
		to   TransferStatus
		want bool
	}{
		{TransferPending, TransferProcessed, true},
		{TransferPending, TransferFailed, true},
		{TransferPending, TransferPending, false},
		{TransferProcessed, TransferFailed, false},
		{TransferProcessed, TransferProcessed, false},
		{TransferFailed, TransferProcessed, false},
		{TransferFailed, TransferFailed, false},
		{TransferFailed, TransferPending, false},
		{TransferStatus("UNKNOWN"), TransferProcessed, false},
	}
	for _, tc := range cases {
		got := tc.from.CanTransitionTo(tc.to)
		if got != tc.want {
			t.Errorf("%s -> %s: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestBuildLedgerEntries_IsBalanced(t *testing.T) {
	entries := BuildLedgerEntries("t1", "walletA", "walletB", 500, "d1", "c1")
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 entries, got %d", len(entries))
	}

	var totalDebit, totalCredit int64
	for _, e := range entries {
		if e.TransferID != "t1" {
			t.Errorf("entry has wrong transfer id: %s", e.TransferID)
		}
		switch e.Type {
		case EntryDebit:
			totalDebit += e.Amount
			if e.WalletID != "walletA" {
				t.Errorf("debit should be on source wallet, got %s", e.WalletID)
			}
		case EntryCredit:
			totalCredit += e.Amount
			if e.WalletID != "walletB" {
				t.Errorf("credit should be on destination wallet, got %s", e.WalletID)
			}
		default:
			t.Errorf("unexpected entry type: %s", e.Type)
		}
	}
	if totalDebit != totalCredit {
		t.Errorf("ledger is not balanced: debit=%d credit=%d", totalDebit, totalCredit)
	}
	if totalDebit != 500 {
		t.Errorf("expected amount 500, got %d", totalDebit)
	}
}
