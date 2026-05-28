package service

import (
	"context"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// WalletService exposes read and seed operations for wallets. It is
// intentionally small; transfer mechanics live in TransferService.
//
// All mutations that depend on a balance read (the actual money flow)
// belong in TransferService so the locking + tx discipline lives in
// exactly one place.
type WalletService struct {
	// exec is the *sql.DB used for non-transactional reads (and the
	// seed insert in Create). Wallet operations are independent —
	// they never participate in a transfer's transaction.
	exec    repository.Executor
	wallets repository.WalletRepository
}

// NewWalletService wires the dependencies. Both arguments must be
// non-nil.
func NewWalletService(exec repository.Executor, wallets repository.WalletRepository) *WalletService {
	return &WalletService{exec: exec, wallets: wallets}
}

// Get returns the wallet for id. The handler maps the
// repository.ErrNotFound result to HTTP 404.
func (s *WalletService) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.wallets.Get(ctx, s.exec, id)
}

// Create seeds a wallet with an initial balance.
//
// This endpoint is a convenience for tests and for bootstrapping a
// local environment; in production it would be replaced with
// provisioning under an admin scope (see AGENTS.md §S-10). The
// validation here is minimal:
//
//   - empty id is rejected (also enforced by the schema's PRIMARY KEY,
//     but failing early with a clearer error is friendlier),
//
//   - negative initial balance is rejected (schema CHECK also enforces
//     balance >= 0, but again — earlier failure, better message).
//
// Duplicate ids surface as a Postgres unique violation propagated
// up from the repository layer.
func (s *WalletService) Create(ctx context.Context, id string, initialBalance int64) (*domain.Wallet, error) {
	if id == "" {
		return nil, domain.ErrWalletNotFound
	}
	if initialBalance < 0 {
		return nil, domain.ErrInvalidAmount
	}
	w := &domain.Wallet{ID: id, Balance: initialBalance}
	if err := s.wallets.Insert(ctx, s.exec, w); err != nil {
		return nil, err
	}
	return w, nil
}
