package model_test

import (
	"testing"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
)

func TestTransfer_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tx      model.Transfer
		wantErr error
	}{
		{
			name: "valid transfer",
			tx: model.Transfer{
				IdempotencyKey: "key-123",
				FromWalletID:   "w_1",
				ToWalletID:     "w_2",
				Amount:         500,
			},
			wantErr: nil,
		},
		{
			name: "missing idempotency key",
			tx: model.Transfer{
				IdempotencyKey: "   ",
				FromWalletID:   "w_1",
				ToWalletID:     "w_2",
				Amount:         500,
			},
			wantErr: model.ErrInvalidIdempotencyKey,
		},
		{
			name: "missing source wallet",
			tx: model.Transfer{
				IdempotencyKey: "key-123",
				FromWalletID:   "",
				ToWalletID:     "w_2",
				Amount:         500,
			},
			wantErr: model.ErrWalletNotFound,
		},
		{
			name: "self transfer",
			tx: model.Transfer{
				IdempotencyKey: "key-123",
				FromWalletID:   "w_1",
				ToWalletID:     "w_1",
				Amount:         500,
			},
			wantErr: model.ErrSelfTransfer,
		},
		{
			name: "zero amount",
			tx: model.Transfer{
				IdempotencyKey: "key-123",
				FromWalletID:   "w_1",
				ToWalletID:     "w_2",
				Amount:         0,
			},
			wantErr: model.ErrInvalidAmount,
		},
		{
			name: "negative amount",
			tx: model.Transfer{
				IdempotencyKey: "key-123",
				FromWalletID:   "w_1",
				ToWalletID:     "w_2",
				Amount:         -100,
			},
			wantErr: model.ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tx.Validate()
			if err != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestComputePayloadHash(t *testing.T) {
	hash1 := model.ComputePayloadHash("w_1", "w_2", 1000)
	hash2 := model.ComputePayloadHash("w_1", "w_2", 1000)
	hash3 := model.ComputePayloadHash("w_1", "w_2", 2000)

	if hash1 != hash2 {
		t.Fatalf("expected identical hashes for same parameters, got %s and %s", hash1, hash2)
	}
	if hash1 == hash3 {
		t.Fatalf("expected different hashes for different amounts, but they matched")
	}
}
