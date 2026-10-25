// Package repository defines the persistence contracts used by the service
// layer. Implementations only persist and query; workflow decisions belong to
// the service and domain layers.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
)

// Store gives access to the repositories, either directly (each call is its own
// statement) or bound to a single database transaction via WithinTx.
type Store interface {
	Repositories

	// WithinTx runs fn inside one transaction: it commits if fn returns nil and
	// rolls back otherwise. Transient failures (deadlock, serialization) are
	// retried by re-running fn in a fresh transaction, so fn must not have side
	// effects outside the Repositories it is given.
	WithinTx(ctx context.Context, fn func(Repositories) error) error
}

// Repositories groups the per-aggregate repositories.
type Repositories interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledger() LedgerRepository
	Idempotency() IdempotencyRepository
}

type WalletRepository interface {
	// Create inserts a new wallet. It returns domain.ErrWalletExists on a duplicate id.
	Create(ctx context.Context, w *domain.Wallet) error
	// Get returns domain.ErrWalletNotFound when the wallet does not exist.
	Get(ctx context.Context, id string) (*domain.Wallet, error)
	// LockForUpdate row-locks the given wallets in ascending id order (so
	// concurrent transfers in opposite directions cannot deadlock) and returns
	// the ones that exist, keyed by id. Must be called inside a transaction.
	LockForUpdate(ctx context.Context, ids ...string) (map[string]*domain.Wallet, error)
	// UpdateBalance persists the wallet's balance.
	UpdateBalance(ctx context.Context, w *domain.Wallet, now time.Time) error
}

type TransferRepository interface {
	Insert(ctx context.Context, t *domain.Transfer) error
	// Get returns domain.ErrTransferNotFound when the transfer does not exist.
	Get(ctx context.Context, id uuid.UUID) (*domain.Transfer, error)
	// SaveTransition persists the status change of t, but only if the stored
	// status is still `from` (guarded update). It returns
	// domain.ErrInvalidTransition when another writer already moved the transfer.
	SaveTransition(ctx context.Context, t *domain.Transfer, from domain.TransferStatus) error
}

type LedgerRepository interface {
	Insert(ctx context.Context, entries ...domain.LedgerEntry) error
	ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error)
}

// IdempotencyRecord ties an idempotency key to the request that first used it
// and the transfer it produced.
type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  *uuid.UUID
	CreatedAt   time.Time
}

type IdempotencyRepository interface {
	// Claim atomically reserves key for requestHash. It reports claimed=false if
	// the key already exists. If another transaction is claiming the same key
	// concurrently, Claim blocks until that transaction commits or rolls back.
	Claim(ctx context.Context, key, requestHash string, now time.Time) (claimed bool, err error)
	// Get returns the record for key, or nil if there is none.
	Get(ctx context.Context, key string) (*IdempotencyRecord, error)
	// AttachTransfer links a claimed key to the transfer it produced.
	AttachTransfer(ctx context.Context, key string, transferID uuid.UUID) error
}
