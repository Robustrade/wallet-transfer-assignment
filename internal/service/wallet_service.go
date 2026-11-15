package service

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

type WalletService struct{}

func (s *WalletService) CreateWallet(ctx context.Context, id string, initialBalance int64) (*domain.Wallet, error) {
	wallet, err := domain.NewWallet(id, initialBalance)
	if err != nil {
		return nil, err
	}
	if err := repository.Repo.CreateWallet(database.DB.WithContext(ctx), wallet); err != nil {
		return nil, err
	}
	return wallet, nil
}
