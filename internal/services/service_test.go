package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/model"
	mockrepo "github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/repository/mock"
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/services"
	"github.com/golang/mock/gomock"
)

func TestCreateTransfer_ValidationError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repoMock := mockrepo.NewMockRepository(ctrl)
	svc := services.NewTransferService(repoMock)

	request := model.CreateTransferRequest{
		IdempotencyKey: "",
		FromWalletID:   "wallet-a",
		ToWalletID:     "wallet-b",
		Amount:         100,
		Status:         "PENDING",
	}

	_, err := svc.CreateTransfer(context.Background(), request)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !errors.Is(err, model.ErrInvalidIdempotencyKey) {
		t.Fatalf("expected ErrInvalidIdempotencyKey, got %v", err)
	}
}

func TestCreateTransfer_RepositoryErrorWrapped(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repoMock := mockrepo.NewMockRepository(ctrl)
	svc := services.NewTransferService(repoMock)

	request := model.CreateTransferRequest{
		IdempotencyKey: "key-123",
		FromWalletID:   "wallet-a",
		ToWalletID:     "wallet-b",
		Amount:         100,
		Status:         "PENDING",
	}

	repoErr := errors.New("database unavailable")
	repoMock.EXPECT().CreateTransfer(gomock.Any(), request).Return(model.Transfer{}, repoErr)

	_, err := svc.CreateTransfer(context.Background(), request)
	if err == nil {
		t.Fatal("expected repository error, got nil")
	}
	if !strings.Contains(err.Error(), "create transfer:") {
		t.Fatalf("expected wrapped error containing 'create transfer:', got %v", err)
	}
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected wrapped error to contain original error, got %v", err)
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repoMock := mockrepo.NewMockRepository(ctrl)
	svc := services.NewTransferService(repoMock)

	request := model.CreateTransferRequest{
		IdempotencyKey: "key-123",
		FromWalletID:   "wallet-a",
		ToWalletID:     "wallet-b",
		Amount:         100,
		Status:         "PENDING",
	}

	expected := model.Transfer{
		ID:             "transfer-1",
		IdempotencyKey: request.IdempotencyKey,
		FromWallet:     request.FromWalletID,
		ToWallet:       request.ToWalletID,
		Amount:         request.Amount,
		Status:         "PROCESSED",
	}

	repoMock.EXPECT().CreateTransfer(gomock.Any(), request).Return(expected, nil)

	actual, err := svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actual != expected {
		t.Fatalf("expected %#v, got %#v", expected, actual)
	}
}
