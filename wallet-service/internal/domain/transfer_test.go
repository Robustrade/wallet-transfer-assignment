package domain

import (
	"testing"
)

func TestTransferRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     TransferRequest
		wantErr error
	}{
		{
			name: "valid request",
			req: TransferRequest{
				IdempotencyKey: "idem1",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			wantErr: nil,
		},
		{
			name: "missing idempotency key",
			req: TransferRequest{
				FromWalletID: "w1",
				ToWalletID:   "w2",
				Amount:       100,
			},
			wantErr: ErrMissingIdempotencyKey,
		},
		{
			name: "missing from wallet",
			req: TransferRequest{
				IdempotencyKey: "idem1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			wantErr: ErrMissingFromWallet,
		},
		{
			name: "missing to wallet",
			req: TransferRequest{
				IdempotencyKey: "idem1",
				FromWalletID:   "w1",
				Amount:         100,
			},
			wantErr: ErrMissingToWallet,
		},
		{
			name: "zero amount",
			req: TransferRequest{
				IdempotencyKey: "idem1",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         0,
			},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "negative amount",
			req: TransferRequest{
				IdempotencyKey: "idem1",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         -50,
			},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "amount exceeds max limit",
			req: TransferRequest{
				IdempotencyKey: "idem1",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         2_000_000_000_000,
			},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "self transfer",
			req: TransferRequest{
				IdempotencyKey: "idem1",
				FromWalletID:   "w1",
				ToWalletID:     "w1",
				Amount:         100,
			},
			wantErr: ErrSelfTransfer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err != tt.wantErr {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestTransfer_IsTerminal(t *testing.T) {
	tests := []struct {
		status   string
		expected bool
	}{
		{TransferStatusPending, false},
		{TransferStatusProcessed, true},
		{TransferStatusFailed, true},
		{"UNKNOWN", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			tr := Transfer{Status: tt.status}
			if got := tr.IsTerminal(); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
