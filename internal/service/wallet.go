package service

import (
	"context"

	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
)

// CreateWallet creates a new wallet with an optional initial balance.
func (s *Service) CreateWallet(ctx context.Context, req CreateWalletRequest) (*domain.Wallet, error) {
	id := req.ID
	if id == "" {
		id = uuid.New().String()
	}
	w := &domain.Wallet{ID: id, Balance: req.InitialBalance}
	if err := s.walletRepo.Create(ctx, s.pool, w); err != nil {
		return nil, err
	}
	return w, nil
}

// GetWallet retrieves a wallet by ID.
func (s *Service) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.walletRepo.GetByID(ctx, s.pool, id)
}

// GetTransferHistory returns all transfers involving the given wallet.
func (s *Service) GetTransferHistory(ctx context.Context, walletID string) ([]domain.Transfer, error) {
	return s.transferRepo.GetByWalletID(ctx, s.pool, walletID)
}
