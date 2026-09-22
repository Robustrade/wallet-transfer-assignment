package service

import (
	"errors"
	"testing"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

func TestCreateTransferInputValidate(t *testing.T) {
	tests := []struct {
		name  string
		input CreateTransferInput
		want  error
	}{
		{
			name: "missing idempotency key",
			input: CreateTransferInput{
				FromWalletID: "wallet-1",
				ToWalletID:   "wallet-2",
				Amount:       1,
			},
			want: domain.ErrIdempotencyKeyRequired,
		},
		{
			name: "same wallet",
			input: CreateTransferInput{
				IdempotencyKey: "key",
				FromWalletID:   "wallet-1",
				ToWalletID:     "wallet-1",
				Amount:         1,
			},
			want: domain.ErrSameWallet,
		},
		{
			name: "non-positive amount",
			input: CreateTransferInput{
				IdempotencyKey: "key",
				FromWalletID:   "wallet-1",
				ToWalletID:     "wallet-2",
				Amount:         0,
			},
			want: domain.ErrInvalidAmount,
		},
		{
			name: "valid input",
			input: CreateTransferInput{
				IdempotencyKey: "key",
				FromWalletID:   "wallet-1",
				ToWalletID:     "wallet-2",
				Amount:         1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.input.Validate()
			if !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
		})
	}
}
