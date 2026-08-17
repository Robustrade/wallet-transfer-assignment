package service

import (
	"context"
	"testing"

	"github.com/raknay/wallet-transfer-assignment/internal/wallet/domain"
	mock_repository "github.com/raknay/wallet-transfer-assignment/internal/wallet/repository/mock"
	"go.uber.org/mock/gomock"
)

func TestCreateTransfer_ValidationError(t *testing.T) {
	ctrl := gomock.NewController(t)

	repository := mock_repository.NewMockWalletRepository(ctrl)
	service := NewWalletService(repository)

	tr := domain.Transfer{
		IdempotencyKey: "",
		FromWallet:     "wallet-a",
		ToWallet:       "wallet-b",
		Amount:         100,
		Status:         "PENDING",
	}
	_, err := service.CreateTransfer(context.Background(), tr)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repository := mock_repository.NewMockWalletRepository(ctrl)
	service := NewWalletService(repository)

	tr := domain.Transfer{
		IdempotencyKey: "key-123",
		FromWallet:     "wallet-1",
		ToWallet:       "wallet-2",
		Amount:         100,
		Status:         "PENDING",
	}

	expected := domain.Transfer{
		ID:             "transfer-1",
		IdempotencyKey: tr.IdempotencyKey,
		FromWallet:     tr.FromWallet,
		ToWallet:       tr.ToWallet,
		Amount:         tr.Amount,
		Status:         "PROCESSED",
	}

	repository.EXPECT().CreateTransfer(gomock.Any(), tr).Return(expected, nil)

	res, err := service.CreateTransfer(context.Background(), tr)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if res != expected {
		t.Fatalf("expected %#v, got %#v", expected, res)
	}
}
