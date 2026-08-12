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

// LedgerPage is a page of a wallet's ledger history. NextBeforeID is nil
// when there is no further page.
type LedgerPage struct {
	Entries      []domain.LedgerEntry
	NextBeforeID *int64
}

// ListEntries returns a wallet's ledger history, newest first. limit is
// clamped to a sane default/maximum; beforeID nil starts from the newest
// entry. It fetches one extra row to determine whether a further page
// exists, rather than guessing from a full page.
func (s *WalletService) ListEntries(ctx context.Context, walletID string, limit int, beforeID *int64) (LedgerPage, error) {
	if limit <= 0 || limit > 100 {
		limit = defaultLedgerPageSize
	}
	if _, err := s.repos.Wallets().Get(ctx, walletID); err != nil {
		return LedgerPage{}, err
	}
	entries, err := s.repos.Ledger().ListByWallet(ctx, walletID, limit+1, beforeID)
	if err != nil {
		return LedgerPage{}, err
	}
	page := LedgerPage{Entries: entries}
	if len(entries) > limit {
		page.Entries = entries[:limit]
		last := page.Entries[len(page.Entries)-1].ID
		page.NextBeforeID = &last
	}
	return page, nil
}
