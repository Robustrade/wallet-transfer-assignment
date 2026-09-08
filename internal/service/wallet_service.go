package service

import (
	"context"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// WalletService exposes read operations over wallets.
type WalletService struct {
	wallets WalletRepository
}

// NewWalletService wires the service.
func NewWalletService(wallets WalletRepository) *WalletService {
	return &WalletService{wallets: wallets}
}

// Balance returns the current balance of a wallet, or domain.ErrWalletNotFound.
func (s *WalletService) Balance(ctx context.Context, walletID string) (*domain.Wallet, error) {
	return s.wallets.Get(ctx, walletID)
}
