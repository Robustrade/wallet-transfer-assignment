package service

import (
	"context"
	"fmt"

	"github.com/Saket121/wallet-transfer-assignment/internal/domain"
	"github.com/Saket121/wallet-transfer-assignment/internal/repository"
)

type WalletService struct {
	repo repository.WalletRepository
}

func NewWalletService(repo repository.WalletRepository) *WalletService {
	return &WalletService{repo: repo}
}

// GetBalance retrieves the current balance for a wallet.
func (s *WalletService) GetBalance(ctx context.Context, walletID string) (*domain.Wallet, error) {
	if walletID == "" {
		return nil, fmt.Errorf("walletId is required")
	}

	wallet, err := s.repo.GetWallet(ctx, walletID)
	if err != nil {
		return nil, fmt.Errorf("getting wallet: %w", err)
	}
	if wallet == nil {
		return nil, fmt.Errorf("wallet not found: %s", walletID)
	}

	return wallet, nil
}
