package service

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

type WalletService interface {
	GetWalletBalance(ctx context.Context, walletID string) (*model.Wallet, error)
}

type walletService struct {
	repo repository.Repository
}

func NewWalletService(repo repository.Repository) WalletService {
	return &walletService{repo: repo}
}

func (s *walletService) GetWalletBalance(ctx context.Context, walletID string) (*model.Wallet, error) {
	// Directly delegates to the existing repository method
	return s.repo.GetWallet(ctx, walletID)
}
