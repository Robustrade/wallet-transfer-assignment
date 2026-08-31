package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestCreateTransferRequestValidateRejectsMalformedInput verifies API input guards.
func TestCreateTransferRequestValidateRejectsMalformedInput(t *testing.T) {
	validID := uuid.NewString()
	tests := []CreateTransferRequest{
		{},
		{IdempotencyKey: "key", FromWalletID: validID, ToWalletID: validID, Amount: 1},
		{IdempotencyKey: "key", FromWalletID: "not-a-uuid", ToWalletID: uuid.NewString(), Amount: 1},
		{IdempotencyKey: "key", FromWalletID: uuid.NewString(), ToWalletID: "not-a-uuid", Amount: 1},
		{IdempotencyKey: "key", FromWalletID: uuid.NewString(), ToWalletID: uuid.NewString(), Amount: 0},
	}
	for _, request := range tests {
		if !errors.Is(request.Validate(), ErrInvalidRequest) {
			t.Fatalf("expected invalid request for %#v", request)
		}
	}
}

// TestCreateTransferRequestValidateAcceptsUUIDWallets verifies valid API input is accepted.
func TestCreateTransferRequestValidateAcceptsUUIDWallets(t *testing.T) {
	request := CreateTransferRequest{IdempotencyKey: "key", FromWalletID: uuid.NewString(), ToWalletID: uuid.NewString(), Amount: 1}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
}
