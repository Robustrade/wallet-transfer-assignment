package service

import (
	"context"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

type WalletService struct {
	repo *repository.WalletRepository
}

func NewWalletService(repo *repository.WalletRepository) *WalletService {
	return &WalletService{repo: repo}
}

// GetWalletBalance orchestrates the fetch and formats the domain response
func (s *WalletService) GetWalletBalance(ctx context.Context, walletID string) (*domain.WalletResponse, error) {
	balance, err := s.repo.GetBalance(ctx, walletID)
	if err != nil {
		return nil, err
	}

	return &domain.WalletResponse{
		WalletId: walletID,
		Balance:  balance,
	}, nil
}
