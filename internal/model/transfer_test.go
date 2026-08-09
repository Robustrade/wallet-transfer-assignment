package model

import (
	"testing"

	db "github.com/chaitanya-bhagat/wallet-transfer-assignment/database/generated"
)

func TestCreateTransferRequest_Validatation(t *testing.T) {
	tests := []struct {
		name    string
		input   CreateTransferRequest
		wantErr error
	}{
		{
			name:    "missing idempotency key",
			input:   CreateTransferRequest{FromWalletID: "wallet-a", ToWalletID: "wallet-b", Amount: 10},
			wantErr: ErrInvalidIdempotencyKey,
		},
		{
			name:    "missing source wallet",
			input:   CreateTransferRequest{IdempotencyKey: "key", ToWalletID: "wallet-b", Amount: 10},
			wantErr: ErrInvalidSourceWallet,
		},
		{
			name:    "missing destination wallet",
			input:   CreateTransferRequest{IdempotencyKey: "key", FromWalletID: "wallet-a", Amount: 10},
			wantErr: ErrInvalidDestinationWallet,
		},
		{
			name:    "same source and destination wallet",
			input:   CreateTransferRequest{IdempotencyKey: "key", FromWalletID: "wallet-a", ToWalletID: "wallet-a", Amount: 10},
			wantErr: ErrSameWallet,
		},
		{
			name:    "invalid amount",
			input:   CreateTransferRequest{IdempotencyKey: "key", FromWalletID: "wallet-a", ToWalletID: "wallet-b", Amount: 0},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "valid request",
			input: CreateTransferRequest{
				IdempotencyKey: "key",
				FromWalletID:   "wallet-a",
				ToWalletID:     "wallet-b",
				Amount:         100,
			},
			wantErr: nil,
		},
		{
			name: "valid request with explicit status",
			input: CreateTransferRequest{
				IdempotencyKey: "key",
				FromWalletID:   "wallet-a",
				ToWalletID:     "wallet-b",
				Amount:         100,
				Status:         "pending",
			},
			wantErr: nil,
		},
		{
			name: "invalid status",
			input: CreateTransferRequest{
				IdempotencyKey: "key",
				FromWalletID:   "wallet-a",
				ToWalletID:     "wallet-b",
				Amount:         100,
				Status:         "INVALID",
			},
			wantErr: ErrInvalidStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validatation()
			if err != tt.wantErr {
				t.Fatalf("Validatation() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestMapToTransfer(t *testing.T) {
	in := db.Transfer{
		ID:             "transfer-1",
		IdempotencyKey: "key-123",
		FromWalletID:   "wallet-a",
		ToWalletID:     "wallet-b",
		Amount:         100,
		Status:         "PROCESSED",
	}

	got := MapToTransfer(in)
	if got.ID != in.ID || got.IdempotencyKey != in.IdempotencyKey || got.FromWallet != in.FromWalletID || got.ToWallet != in.ToWalletID || got.Amount != in.Amount || got.Status != in.Status {
		t.Fatalf("MapToTransfer() = %+v, want %+v", got, in)
	}
}
