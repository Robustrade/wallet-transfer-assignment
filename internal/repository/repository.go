// Package repository defines persistence-facing interfaces used by the
// service layer. The concrete implementation lives in the postgres subpackage;
// the service layer depends only on these interfaces.
package repository

import (
	"context"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

// WalletRepository persists and retrieves wallets.
type WalletRepository interface {
	// Create inserts a brand new wallet. It returns domain.ErrWalletAlreadyExists
	// if the id is already taken.
	Create(ctx context.Context, wallet *domain.Wallet) error

	// Get returns a wallet without acquiring a row lock, suitable for
	// read-only queries such as balance lookups.
	Get(ctx context.Context, id string) (*domain.Wallet, error)

	// GetForUpdate returns a wallet and acquires a row-level lock that is
	// held until the enclosing transaction commits or rolls back. Callers
	// that need to lock more than one wallet must fetch them in a
	// consistent global order (e.g. by ID) to avoid deadlocks.
	GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error)

	// UpdateBalance persists a new balance for the given wallet. It must
	// only be called while the caller holds the row lock obtained from
	// GetForUpdate within the same transaction.
	UpdateBalance(ctx context.Context, id string, newBalance int64) error
}

// TransferRepository persists and retrieves transfers.
type TransferRepository interface {
	Create(ctx context.Context, transfer *domain.Transfer) error
	UpdateState(ctx context.Context, id string, state domain.TransferState, failureReason string) error
	GetByID(ctx context.Context, id string) (*domain.Transfer, error)
	ListByWallet(ctx context.Context, walletID string) ([]domain.Transfer, error)
}

// LedgerRepository persists and retrieves double-entry ledger rows.
type LedgerRepository interface {
	CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error
	ListByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
	ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

// IdempotencyRepository persists idempotency claims so duplicate requests
// can be detected and safely replayed.
type IdempotencyRepository interface {
	// Claim attempts to atomically reserve requestHash for key.
	//
	// If no record exists for key, one is created and claimed=true is
	// returned so the caller may proceed with processing.
	//
	// If a record already exists, claimed=false is returned along with the
	// existing record so the caller can either replay a completed response
	// or detect an in-flight/conflicting duplicate.
	Claim(ctx context.Context, key, requestHash string) (claimed bool, existing *domain.IdempotencyRecord, err error)

	// Complete stores the final outcome of the request identified by key so
	// future duplicates can be replayed without re-executing side effects.
	Complete(ctx context.Context, key, transferID string, responseStatus int, responseBody []byte) error
}

// Repositories bundles all repositories reachable within a single unit of
// work (i.e. sharing the same database transaction).
type Repositories struct {
	Wallets     WalletRepository
	Transfers   TransferRepository
	Ledger      LedgerRepository
	Idempotency IdempotencyRepository
}

// UnitOfWork executes fn within a single atomic transaction. If fn returns
// an error, all changes made through repos are rolled back; otherwise they
// are committed. Implementations must be safe for concurrent use.
type UnitOfWork interface {
	Execute(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}
