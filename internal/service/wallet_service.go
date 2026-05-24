package service

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
)

type WalletRepository interface {
	Create(ctx context.Context, wallet *domain.Wallet) (*domain.Wallet, error)
	Get(ctx context.Context, walletID string) (*domain.Wallet, error)
}

type WalletService struct {
	walletRepository WalletRepository
}

func NewWalletService(walletRepository WalletRepository) *WalletService {
	return &WalletService{walletRepository: walletRepository}
}

type CreateWalletRequest struct {
	WalletID       domain.WalletID
	InitialBalance int64
}

func (w *WalletService) CreateWallet(ctx context.Context, req CreateWalletRequest) (*domain.Wallet, error) {
	if req.InitialBalance < 0 {
		return nil, domain.ErrInvalidAmount
	}

	wallet := &domain.Wallet{
		ID:      req.WalletID,
		Balance: req.InitialBalance,
	}

	return w.walletRepository.Create(ctx, wallet)
}

func (w *WalletService) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {

	return w.walletRepository.Get(ctx, walletID)
}
