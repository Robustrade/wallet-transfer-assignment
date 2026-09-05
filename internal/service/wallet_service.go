package service

import (
	"context"

	"wallet-transfer/internal/domain"
)

type WalletService struct {
	wallets   WalletRepository
	transfers TransferRepository
}

func NewWalletService(wallets WalletRepository, transfers TransferRepository) *WalletService {
	return &WalletService{wallets: wallets, transfers: transfers}
}

func (s *WalletService) Get(ctx context.Context, id string) (domain.Wallet, error) {
	id = domain.NormalizeWalletID(id)
	if err := domain.ValidateWalletID("walletId", id); err != nil {
		return domain.Wallet{}, err
	}
	return s.wallets.Get(ctx, id)
}

func (s *WalletService) History(ctx context.Context, id string, limit int) ([]domain.Transfer, error) {
	id = domain.NormalizeWalletID(id)
	if err := domain.ValidateWalletID("walletId", id); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if _, err := s.wallets.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.transfers.ListByWallet(ctx, id, limit)
}
