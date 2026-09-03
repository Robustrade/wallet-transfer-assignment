package service

import (
	"context"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

func (s *Service) CreateWallet(ctx context.Context, id string, initialBalance int64) (*domain.Wallet, error) {
	if initialBalance < 0 {
		return nil, fmt.Errorf("%w: initialBalance must not be negative", domain.ErrInvalidAmount)
	}
	if id == "" {
		id = domain.NewID("wallet")
	}

	timestamp := now()
	wallet := &domain.Wallet{ID: id, Balance: initialBalance, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := s.store.WithTx(ctx, func(tx *repository.Tx) error {
		return tx.CreateWallet(ctx, wallet)
	}); err != nil {
		return nil, err
	}
	return wallet, nil
}

func (s *Service) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.store.GetWallet(ctx, id)
}
