package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// WalletService handles wallet reads and the demo wallet-creation helper.
type WalletService struct {
	db      *sql.DB
	wallets repository.WalletRepository
	ledger  repository.LedgerRepository
}

// NewWalletService builds the service.
func NewWalletService(conn *sql.DB) *WalletService {
	return &WalletService{db: conn}
}

// CreateWallet is a demo/test helper that creates a wallet with an opening balance.
// A real system would not let clients create money.
func (s *WalletService) CreateWallet(ctx context.Context, id string, initialBalance int64) (domain.Wallet, error) {
	if id == "" || len(id) > 64 {
		return domain.Wallet{}, fmt.Errorf("%w: id must be 1-64 characters", domain.ErrValidation)
	}
	if initialBalance < 0 || initialBalance > domain.MaxAmount {
		return domain.Wallet{}, fmt.Errorf("%w: initialBalance must be between 0 and %d",
			domain.ErrValidation, domain.MaxAmount)
	}
	w, err := s.wallets.Create(ctx, s.db, id, initialBalance)
	if err != nil {
		return domain.Wallet{}, err
	}
	if w == nil {
		return domain.Wallet{}, fmt.Errorf("%w: %s", domain.ErrWalletAlreadyExists, id)
	}
	return *w, nil
}

// GetWallet returns one wallet with its balance.
func (s *WalletService) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	w, err := s.wallets.FindByID(ctx, s.db, id)
	if err != nil {
		return domain.Wallet{}, err
	}
	if w == nil {
		return domain.Wallet{}, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
	}
	return *w, nil
}

// GetLedger returns the newest ledger entries of a wallet.
func (s *WalletService) GetLedger(ctx context.Context, walletID string, limit int) ([]domain.LedgerEntry, error) {
	if _, err := s.GetWallet(ctx, walletID); err != nil {
		return nil, err
	}
	return s.ledger.FindByWalletID(ctx, s.db, walletID, limit)
}
