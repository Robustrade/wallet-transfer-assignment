package service

import (
	"context"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/repository"
)

type WalletService struct {
	store repository.Store
}

func NewWalletService(store repository.Store) *WalletService {
	return &WalletService{store: store}
}

func (s *WalletService) CreateWallet(ctx context.Context, id string, initialBalance int64) (*domain.Wallet, error) {
	w, err := domain.NewWallet(id, initialBalance)
	if err != nil {
		return nil, err
	}
	if err := s.store.Wallets().Create(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.store.Wallets().Get(ctx, id)
}
