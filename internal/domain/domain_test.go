package domain_test

import (
	"testing"

	"github.com/ankit729/wallet-transfer/internal/domain"
)

func TestTransferRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		request domain.TransferRequest
		valid   bool
	}{
		{
			name: "valid",
			request: domain.TransferRequest{
				IdempotencyKey: "key-1",
				FromWalletID:   "wallet-1",
				ToWalletID:     "wallet-2",
				Amount:         100,
			},
			valid: true,
		},
		{
			name: "missing idempotency key",
			request: domain.TransferRequest{
				FromWalletID: "wallet-1",
				ToWalletID:   "wallet-2",
				Amount:       100,
			},
			valid: false,
		},
		{
			name: "same wallet",
			request: domain.TransferRequest{
				IdempotencyKey: "key-1",
				FromWalletID:   "wallet-1",
				ToWalletID:     "wallet-1",
				Amount:         100,
			},
			valid: false,
		},
		{
			name: "zero amount",
			request: domain.TransferRequest{
				IdempotencyKey: "key-1",
				FromWalletID:   "wallet-1",
				ToWalletID:     "wallet-2",
				Amount:         0,
			},
			valid: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()

			if test.valid && err != nil {
				t.Fatalf("expected valid request, got error: %v", err)
			}

			if !test.valid && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
