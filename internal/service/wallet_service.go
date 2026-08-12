package service

import (
	"context"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

const defaultLedgerPageSize = 20

// WalletService handles wallet creation and reads. Wallet creation is a
// single insert guarded by the primary key, so unlike TransferService it has
// no idempotency or multi-statement transactional concerns of its own.
type WalletService struct {
	repos Repos
}

// NewWalletService builds a WalletService backed by repos.
func NewWalletService(repos Repos) *WalletService {
	return &WalletService{repos: repos}
}

// Create inserts a new zero-balance USER wallet. Returns
// domain.ErrWalletAlreadyExists if id is taken.
func (s *WalletService) Create(ctx context.Context, id, currency string) (domain.Wallet, error) {
	w, err := domain.NewWallet(id, currency)
	if err != nil {
		return domain.Wallet{}, err
	}
	if err := s.repos.Wallets().Create(ctx, w); err != nil {
		return domain.Wallet{}, err
	}
	return s.repos.Wallets().Get(ctx, id)
}

// Get returns a wallet's current state, including its balance.
func (s *WalletService) Get(ctx context.Context, id string) (domain.Wallet, error) {
	return s.repos.Wallets().Get(ctx, id)
}

// ListEntries returns a wallet's ledger history, newest first. limit is
// clamped to a sane default/maximum; beforeID nil starts from the newest
// entry.
func (s *WalletService) ListEntries(ctx context.Context, walletID string, limit int, beforeID *int64) ([]domain.LedgerEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = defaultLedgerPageSize
	}
	if _, err := s.repos.Wallets().Get(ctx, walletID); err != nil {
		return nil, err
	}
	return s.repos.Ledger().ListByWallet(ctx, walletID, limit, beforeID)
}
