package service

import (
	"context"
	"time"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository"
)

// WalletService creates wallets and reads balances.
type WalletService struct {
	store repository.Store
	now   func() time.Time
}

// NewWalletService builds a WalletService on top of store.
func NewWalletService(store repository.Store) *WalletService {
	return &WalletService{store: store, now: utcMicros}
}

// CreateWallet opens a wallet with openingBalance minor units.
func (s *WalletService) CreateWallet(ctx context.Context, id string, openingBalance int64) (*domain.Wallet, error) {
	w, err := domain.NewWallet(id, openingBalance, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.store.Wallets().Create(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

// GetWallet returns a wallet and its current balance.
func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	if err := domain.ValidateWalletID(id); err != nil {
		return nil, err
	}
	return s.store.Wallets().Get(ctx, id)
}
