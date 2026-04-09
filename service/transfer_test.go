package service

import (
	"context"
	"testing"

	"wallet-transfer-assignment/db"
	"wallet-transfer-assignment/model"
)

func TestInvalidWallet(t *testing.T) {

	// init DB first
	db.InitDB()
	db.InitSchema()

	req := model.TransferRequest{
		IdempotencyKey: "test-invalid",
		FromWalletID:   "invalid_wallet",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	_, err := ProcessTransfer(context.Background(), req)

	if err == nil {
		t.Error("expected error for invalid wallet, got nil")
	}
}
