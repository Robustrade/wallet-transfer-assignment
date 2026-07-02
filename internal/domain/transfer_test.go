package domain

import (
	"errors"
	"testing"
)

func TestTransferRequestValidate(t *testing.T) {
	valid := TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet-a",
		ToWalletID:     "wallet-b",
		Amount:         100,
	}

	tests := []struct {
		name    string
		req     TransferRequest
		wantErr error
	}{
		{
			name: "valid request",
			req:  valid,
		},
		{
			name:    "empty idempotency key",
			req:     TransferRequest{FromWalletID: "a", ToWalletID: "b", Amount: 1},
			wantErr: ErrEmptyIdempotency,
		},
		{
			name:    "blank idempotency key",
			req:     TransferRequest{IdempotencyKey: "  ", FromWalletID: "a", ToWalletID: "b", Amount: 1},
			wantErr: ErrEmptyIdempotency,
		},
		{
			name:    "empty from wallet",
			req:     TransferRequest{IdempotencyKey: "k", ToWalletID: "b", Amount: 1},
			wantErr: ErrEmptyWalletID,
		},
		{
			name:    "empty to wallet",
			req:     TransferRequest{IdempotencyKey: "k", FromWalletID: "a", Amount: 1},
			wantErr: ErrEmptyWalletID,
		},
		{
			name:    "same wallet",
			req:     TransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "a", Amount: 1},
			wantErr: ErrSameWallet,
		},
		{
			name:    "zero amount",
			req:     TransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 0},
			wantErr: ErrInvalidAmount,
		},
		{
			name:    "negative amount",
			req:     TransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: -10},
			wantErr: ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}
