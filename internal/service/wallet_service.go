package service

import (
	"context"
	"log/slog"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
)

// WalletService implements wallet management and read use cases. It is kept
// separate from TransferService so the two aggregates can evolve
// independently (Single Responsibility).
type WalletService struct {
	store  repository.Store
	newID  IDGenerator
	logger *slog.Logger
}

// NewWalletService wires a wallet service. A nil idGen falls back to UUIDv4 and
// a nil logger to slog.Default, so callers can pass only what they need.
func NewWalletService(store repository.Store, idGen IDGenerator, logger *slog.Logger) *WalletService {
	if idGen == nil {
		idGen = func() string { return uuid.NewString() }
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &WalletService{store: store, newID: idGen, logger: logger}
}

// CreateWallet provisions a new wallet with a server-generated id and the given
// opening balance.
//
// The id is generated here rather than supplied by the client: callers never
// have to guess a free id, two concurrent creates can never collide, and ids
// cannot be enumerated or squatted on. The new id is returned to the caller
// (and surfaced via the Location header at the HTTP layer).
func (s *WalletService) CreateWallet(ctx context.Context, openingBalance int64, currency string) (*domain.Wallet, error) {
	wallet, err := domain.NewWallet(s.newID(), openingBalance, currency)
	if err != nil {
		return nil, err
	}
	err = s.store.WithinTx(ctx, func(ctx context.Context, repos repository.Repositories) error {
		return repos.Wallets.Create(ctx, wallet)
	})
	if err != nil {
		return nil, err
	}
	s.logger.InfoContext(ctx, "wallet created",
		slog.String("wallet_id", wallet.ID),
		slog.Int64("opening_balance", wallet.Balance),
	)
	return wallet, nil
}

// GetWallet returns a wallet, including its current balance.
func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.store.Reader().Wallets.Get(ctx, id)
}

// ListLedger returns the double-entry ledger history for a wallet. The wallet
// must exist; otherwise domain.ErrWalletNotFound is returned.
func (s *WalletService) ListLedger(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	reader := s.store.Reader()
	if _, err := reader.Wallets.Get(ctx, walletID); err != nil {
		return nil, err
	}
	return reader.Ledger.ListByWallet(ctx, walletID)
}
