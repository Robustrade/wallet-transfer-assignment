// Package repository defines the persistence contract used by the service.
// SQL, driver errors, and connection/transaction handles belong in postgres.
package repository

import (
	"context"
	"errors"
	"time"

	"wallet-transfer-assignment/internal/domain"
)

var (
	// ErrNotFound means a requested row (or one of the requested wallets) is absent.
	ErrNotFound = errors.New("repository: not found")
	// ErrConflict means a uniqueness or guarded-write precondition was violated.
	// A normal duplicate key reservation uses reserved=false instead.
	ErrConflict = errors.New("repository: conflict")
)

// Result contains the original response for replay, not a newly computed result.
// Body preserves the serialized response exactly. StatusCode maps to the stored
// response_status column; no HTTP package is needed by this contract.
type Result struct {
	StatusCode int
	Body       string
}

// IdempotencyRecord reserves a key for a pre-generated transfer ID. Result is nil
// while the transaction is in progress and must be populated before commit.
// Fingerprint is the canonical lowercase SHA-256 defined in migrations/README.md.
type IdempotencyRecord struct {
	Key         string
	Fingerprint string
	TransferID  string
	Result      *Result
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UnitOfWork lets the service choose the atomic boundary by choosing the callback
// contents. The PostgreSQL implementation must begin exactly one READ COMMITTED
// *sql.Tx, bind every Tx operation to it, and commit only when fn returns nil.
// Callback errors cause rollback and remain discoverable with errors.Is/As.
// Panics cause rollback and are re-panicked. Begin and commit errors are returned;
// a commit error must never be represented as a safely persisted FAILED transfer.
// No automatic retries: callbacks may not be safe to execute more than once.
// The callback is invoked synchronously at most once, using the supplied context.
// A successful return means commit succeeded, including deferred constraints.
type UnitOfWork interface {
	WithinTransaction(ctx context.Context, fn func(context.Context, Tx) error) error
}

// Tx is a transaction-scoped repository, not a transaction controller. It exposes
// no Begin, Commit, or Rollback. Implementations must use the same *sql.Tx for all
// calls, never fall back to *sql.DB or open an internal transaction. Do not retain
// Tx after the callback, use it concurrently, or nest units of work. All database
// operations use the supplied context. IDs and timestamps come from the caller.
// Read-only lookups can use a short unit of work; no extra reader interface is
// needed until a use case justifies one.
type Tx interface {
	// GetWallet reads without locking; do not use its balance for financial writes.
	GetWallet(ctx context.Context, id string) (domain.Wallet, error)

	// LockWallets rejects equal IDs with ErrConflict, locks both rows FOR UPDATE in
	// deterministic wallet-ID order, and returns them in argument order. Missing
	// either returns ErrNotFound. Acquired locks last until transaction completion.
	LockWallets(ctx context.Context, fromID, toID string) (from, to domain.Wallet, err error)

	// UpdateWalletBalance sets the absolute minor-unit balance and updated_at.
	// The service must first lock this wallet in the same unit of work. Negative
	// balances fail; missing rows return ErrNotFound. No arithmetic uses floats.
	UpdateWalletBalance(ctx context.Context, id string, balance int64, updatedAt time.Time) error

	// CreateTransfer accepts only a valid PENDING transfer. Empty IdempotencyKey
	// maps to SQL NULL. Lock wallets before insertion to avoid FK lock upgrades.
	CreateTransfer(ctx context.Context, transfer domain.Transfer) error
	GetTransfer(ctx context.Context, id string) (domain.Transfer, error)

	// These guarded writes only allow PENDING -> the named terminal state and
	// update updated_at. No matching PENDING row returns ErrConflict (including a
	// missing ID); there is deliberately no generic status setter.
	MarkTransferProcessed(ctx context.Context, id string, updatedAt time.Time) error
	MarkTransferFailed(ctx context.Context, id string, updatedAt time.Time) error

	// GetTransferByIdempotencyKey follows the record's transfer reference; it must
	// not infer a result from an unrelated transfer. Missing record/transfer returns
	// ErrNotFound. Use GetIdempotencyRecord to obtain fingerprint and replay body.
	GetTransferByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error)

	// CreateLedgerEntry inserts one valid entry; duplicate ID or transfer/type is
	// ErrConflict. The service inserts both entries and marks PROCESSED atomically.
	CreateLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error
	// ListLedgerEntries returns entries ordered by entry_type, then id. No entries
	// yields an empty slice and nil error, including for an unknown transfer ID.
	ListLedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)

	// ReserveIdempotencyKey creates a claim with no result and the supplied transfer
	// ID/timestamps. Use a targeted key-conflict operation that leaves the SQL tx
	// usable: an existing key returns false, nil without changing its record.
	// Other failures return errors. After false, read the record in a subsequent
	// statement and let the service compare fingerprints and replay or conflict.
	// The referenced transfer must exist before commit (deferred schema constraint).
	ReserveIdempotencyKey(ctx context.Context, key, fingerprint, transferID string, createdAt time.Time) (reserved bool, err error)
	GetIdempotencyRecord(ctx context.Context, key string) (IdempotencyRecord, error)

	// CompleteIdempotencyKey attaches the final replay result to the reserved
	// transfer. Match key AND transferID, require both stored response fields NULL,
	// and update updated_at. A missing/mismatched/already-completed claim returns
	// ErrConflict; never overwrite an original result. The service must first
	// persist the terminal transfer state in this same unit of work.
	CompleteIdempotencyKey(ctx context.Context, key, transferID string, result Result, updatedAt time.Time) error
}

// ErrTransactionFailed requires leaving the callback and rolling back. In
// particular, a unique violation can match both this and ErrConflict; callers
// must never treat that as permission to continue in the aborted transaction.
var ErrTransactionFailed = errors.New("repository: transaction failed")

// ErrRetryableTransaction identifies deadlock/serialization failures. Any retry
// must start a new whole unit of work; the adapter does not retry automatically.
var ErrRetryableTransaction = errors.New("repository: retryable transaction failure")
