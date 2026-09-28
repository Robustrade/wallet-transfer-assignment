package service

import (
	"context"
	"fmt"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

// Limits for transfer history.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// WalletService serves the read APIs. It reads directly from the pool, not in
// a write transaction. On SQLite, a BEGIN IMMEDIATE transaction would make
// every read queue behind the writes.
type WalletService struct {
	wallets   WalletRepository
	transfers TransferRepository
}

// NewWalletService takes repositories bound to the connection pool.
func NewWalletService(r Repos) *WalletService {
	return &WalletService{wallets: r.Wallets, transfers: r.Transfers}
}

// GetWallet returns the wallet's current committed state, or
// domain.ErrWalletNotFound.
func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.wallets.Get(ctx, id)
}

// ListTransfers returns up to limit transfers where the wallet is sender or
// receiver, newest first. A missing wallet is ErrWalletNotFound, rather than
// an empty list, so a typo in the id is not mistaken for "no activity".
func (s *WalletService) ListTransfers(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	if limit < 1 || limit > MaxListLimit {
		return nil, &ValidationError{Field: "limit", Message: fmt.Sprintf("must be between 1 and %d", MaxListLimit)}
	}
	if _, err := s.wallets.Get(ctx, walletID); err != nil {
		return nil, err
	}
	return s.transfers.ListByWallet(ctx, walletID, limit)
}
