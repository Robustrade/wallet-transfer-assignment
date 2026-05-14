package handler_test

import (
	"context"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type mockService struct {
	createWalletFn    func(ctx context.Context, req service.CreateWalletRequest) (*domain.Wallet, error)
	getWalletFn       func(ctx context.Context, id string) (*domain.Wallet, error)
	getTransferHistFn func(ctx context.Context, walletID string) ([]domain.Transfer, error)
	createTransferFn  func(ctx context.Context, req service.CreateTransferRequest) (*domain.Transfer, error)
}

func (m *mockService) CreateWallet(ctx context.Context, req service.CreateWalletRequest) (*domain.Wallet, error) {
	return m.createWalletFn(ctx, req)
}
func (m *mockService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return m.getWalletFn(ctx, id)
}
func (m *mockService) GetTransferHistory(ctx context.Context, walletID string) ([]domain.Transfer, error) {
	return m.getTransferHistFn(ctx, walletID)
}
func (m *mockService) CreateTransfer(ctx context.Context, req service.CreateTransferRequest) (*domain.Transfer, error) {
	return m.createTransferFn(ctx, req)
}
