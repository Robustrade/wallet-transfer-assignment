// Package service contains the application logic: transfer orchestration,
// idempotency and the transaction boundary. It depends on the interfaces
// below, and the repository package implements them. That way the service
// never imports database/sql or a driver.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

// ErrUnavailable means the request could not complete because of a
// transient infrastructure condition: lock timeout, retries exhausted,
// context deadline or cancellation. The handler maps it to
// 503 TEMPORARILY_UNAVAILABLE. Nothing was committed, so the client may safely
// retry with the same idempotency key.
//
// It lives here rather than in domain because it describes infrastructure,
// not a business rule.
var ErrUnavailable = errors.New("temporarily unavailable")

// ErrStaleWrite means a guarded write found the row changed since it was
// read. With the locking used here that cannot happen, so it signals a bug
// (for example, a balance update without GetForUpdate). The transaction rolls
// back and the handler answers 500.
var ErrStaleWrite = errors.New("row changed since it was read")

// ErrNotFound is returned by repository lookups (other than wallets) when no
// row matches. Wallet lookups return domain.ErrWalletNotFound instead,
// because that one is a client-facing business error.
var ErrNotFound = errors.New("record not found")

// TransferResult is the outcome of CreateTransfer. Replayed is true when the
// transfer was created by an earlier request with the same idempotency key.
type TransferResult struct {
	Transfer domain.Transfer
	Replayed bool
}

// IdempotencyRecord ties an idempotency key to the request it was first
// used with and to the transfer that request produced.
type IdempotencyRecord struct {
	Key         string
	RequestHash string
	// TransferID is NULL only inside the transaction that inserted the
	// record. It is always set by the time that transaction commits.
	TransferID uuid.NullUUID
	CreatedAt  time.Time
}

// WalletRepository persists wallets.
type WalletRepository interface {
	// Get reads a wallet without locking it. Used by read APIs.
	// Returns domain.ErrWalletNotFound if the wallet does not exist.
	Get(ctx context.Context, id string) (*domain.Wallet, error)

	// GetForUpdate reads a wallet and locks its row until the transaction
	// ends (Postgres SELECT ... FOR UPDATE; SQLite already holds the write
	// lock). Callers that lock several wallets must do so in sorted id
	// order to avoid deadlocks.
	// Returns domain.ErrWalletNotFound if the wallet does not exist.
	GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error)

	// UpdateBalance changes a wallet's balance from expected to balance.
	//
	// It is a guarded write: the row is updated only if its balance is still
	// expected (the value read under GetForUpdate). Callers are meant to hold
	// the row lock. The guard makes a missing lock fail loudly, with
	// ErrStaleWrite, instead of silently losing a concurrent update.
	// Returns domain.ErrWalletNotFound if the wallet does not exist.
	UpdateBalance(ctx context.Context, id string, expected, balance int64, updatedAt time.Time) error
}

// TransferRepository persists transfers.
type TransferRepository interface {
	// Create inserts a new transfer. Its CreatedAt is also used as the
	// initial updated_at.
	Create(ctx context.Context, t *domain.Transfer) error

	// GetByID returns ErrNotFound if no transfer has this id.
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Transfer, error)

	// UpdateStatus persists t.Status and t.FailureReason using a guarded
	// update that only matches a row still in PENDING:
	//   UPDATE transfers SET ... WHERE id = ? AND status = 'PENDING'
	// If no row matches, it returns domain.ErrInvalidTransition.
	UpdateStatus(ctx context.Context, t *domain.Transfer, updatedAt time.Time) error

	// ListByWallet returns up to limit transfers where the wallet is sender
	// or receiver, newest first.
	ListByWallet(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error)
}

// LedgerRepository persists ledger entries. Entries are append-only.
type LedgerRepository interface {
	Insert(ctx context.Context, entries ...domain.LedgerEntry) error
}

// IdempotencyRepository persists idempotency records.
type IdempotencyRepository interface {
	// Insert claims key for this request (INSERT ... ON CONFLICT DO NOTHING).
	// inserted is false if the key already exists. That includes a key
	// held by a concurrent transaction: the insert waits for that
	// transaction to finish, then reports false if it committed, or
	// inserts if it rolled back.
	Insert(ctx context.Context, key, requestHash string, createdAt time.Time) (inserted bool, err error)

	// Get returns ErrNotFound if the key does not exist.
	Get(ctx context.Context, key string) (*IdempotencyRecord, error)

	// LinkTransfer records which transfer the key produced.
	LinkTransfer(ctx context.Context, key string, transferID uuid.UUID) error
}

// Repos groups the repositories that share one transaction.
type Repos struct {
	Wallets     WalletRepository
	Transfers   TransferRepository
	Ledger      LedgerRepository
	Idempotency IdempotencyRepository
}

// TxManager runs a unit of work inside one database transaction.
type TxManager interface {
	// WithinTx begins a transaction, calls fn with repositories bound to it,
	// then commits. If fn returns an error, the transaction is rolled back
	// and that error is returned.
	//
	// fn may be called MORE THAN ONCE. On a retryable conflict (deadlock,
	// serialization failure) the whole transaction is retried from scratch.
	// fn must therefore only touch the database through the given Repos,
	// and must not perform other side effects (logging, HTTP calls,
	// appending to outer slices).
	//
	// Transient failures (retries exhausted, lock timeout, ctx
	// deadline/cancel) are returned wrapped in ErrUnavailable.
	WithinTx(ctx context.Context, fn func(Repos) error) error
}
