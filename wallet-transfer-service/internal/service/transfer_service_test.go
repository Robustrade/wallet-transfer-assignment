package service

import (
	"testing"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/google/uuid"
)

func TestValidateRequest(t *testing.T) {
	a := uuid.New()
	b := uuid.New()

	tests := []struct {
		name    string
		req     domain.CreateTransferRequest
		wantErr bool
	}{
		{"valid", domain.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: a, ToWalletID: b, Amount: 100}, false},
		{"missing key", domain.CreateTransferRequest{FromWalletID: a, ToWalletID: b, Amount: 100}, true},
		{"same wallet", domain.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: a, ToWalletID: a, Amount: 100}, true},
		{"zero amount", domain.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: a, ToWalletID: b, Amount: 0}, true},
		{"negative amount", domain.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: a, ToWalletID: b, Amount: -1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequestHashIgnoresIdempotencyKey(t *testing.T) {
	a := uuid.New()
	b := uuid.New()

	h1, err := requestHash(domain.CreateTransferRequest{IdempotencyKey: "one", FromWalletID: a, ToWalletID: b, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := requestHash(domain.CreateTransferRequest{IdempotencyKey: "two", FromWalletID: a, ToWalletID: b, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("same request fields produced different hashes: %s != %s", h1, h2)
	}
}
