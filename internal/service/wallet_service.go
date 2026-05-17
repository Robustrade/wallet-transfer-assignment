package service

import (
	"context"
	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

type WalletService struct {
	walletRepo *repository.WalletRepository
}

func NewWalletService(walletRepo *repository.WalletRepository) *WalletService {
	return &WalletService{
		walletRepo: walletRepo,
	}
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.walletRepo.GetWalletByID(ctx, id)
}
