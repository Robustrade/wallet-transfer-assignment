package utils_test

import (
	"testing"

	"wallet-transfer-assigment/models"
	"wallet-transfer-assigment/utils"
)

func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name  string
		value models.TransferRequest
		valid bool
	}{
		{
			name: "valid transfer request",
			value: models.TransferRequest{
				IdempotencyKey: "request_1",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         100,
			},
			valid: true,
		},
		{
			name: "missing required field",
			value: models.TransferRequest{
				IdempotencyKey: "request_1",
				FromWalletID:   "wallet_1",
				Amount:         100,
			},
		},
		{
			name: "zero amount",
			value: models.TransferRequest{
				IdempotencyKey: "request_1",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
			},
		},
		{
			name: "negative amount",
			value: models.TransferRequest{
				IdempotencyKey: "request_1",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         -1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := utils.ValidateRequest(test.value)
			if test.valid && err != nil {
				t.Fatalf("valid request failed validation: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid request passed validation")
			}
		})
	}
}
