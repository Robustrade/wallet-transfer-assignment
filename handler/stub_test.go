package handler_test

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/dto"
	"github.com/google/uuid"
)

// stubService returns fixed results for HTTP mapping tests (no database).
type stubService struct {
	createWalletView  dto.WalletView
	createWalletErr   error
	getWalletView     dto.WalletView
	getWalletErr      error
	createTransfer    dto.TransferOutcome
	createTransferErr error
	getTransferView   dto.TransferView
	getTransferErr    error
}

func (s *stubService) CreateWallet(context.Context, string, int64) (dto.WalletView, error) {
	return s.createWalletView, s.createWalletErr
}
func (s *stubService) GetWallet(context.Context, string) (dto.WalletView, error) {
	return s.getWalletView, s.getWalletErr
}
func (s *stubService) CreateTransfer(context.Context, string, string, string, int64) (dto.TransferOutcome, error) {
	return s.createTransfer, s.createTransferErr
}
func (s *stubService) GetTransfer(context.Context, uuid.UUID) (dto.TransferView, error) {
	return s.getTransferView, s.getTransferErr
}
