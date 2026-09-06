// Package repository defines persistence-facing interfaces used by the
// service layer. Implementations live in sub-packages (e.g. sqlite) and
// contain no business rules — only data access.
package repository

import (
	"context"
	"time"

	"wallet-transfer-assignment/internal/domain"
)

var ErrIdempotencyKeyExists = errDuplicateIdempotencyKey{}

type errDuplicateIdempotencyKey struct{}

func (errDuplicateIdempotencyKey) Error() string { return "idempotency key already exists" }

type WalletRepo interface {
	Get(ctx context.Context, id string) (*domain.Wallet, error)
	// LockForUpdate locks the wallet row for the duration of the enclosing
	// transaction so concurrent transfers touching the same wallet
	// serialize on it. Callers must lock wallets in a consistent order
	// (see UnitOfWork.RunWriteTx) to avoid deadlocks.
	LockForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, id string, newBalance int64, now time.Time) error
	Create(ctx context.Context, wallet *domain.Wallet) error
}

type TransferRepo interface {
	// Insert creates a new PENDING transfer. Returns ErrIdempotencyKeyExists
	// if the idempotency key is already in use.
	Insert(ctx context.Context, t *domain.Transfer) error
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
	GetByID(ctx context.Context, id string) (*domain.Transfer, error)
	UpdateStatus(ctx context.Context, t *domain.Transfer) error
}

type LedgerRepo interface {
	InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error
	ListByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

// Repos bundles repositories bound to a single unit of work (transaction).
type Repos struct {
	Wallets   WalletRepo
	Transfers TransferRepo
	Ledger    LedgerRepo
}

// UnitOfWork runs a function within a single write transaction, giving it
// repositories scoped to that transaction. The transaction commits if fn
// returns nil, and rolls back otherwise.
type UnitOfWork interface {
	RunWriteTx(ctx context.Context, fn func(ctx context.Context, repos Repos) error) error
	// RunReadTx runs fn in a read-only transaction, for queries that don't
	// need the exclusive write lock RunWriteTx takes.
	RunReadTx(ctx context.Context, fn func(ctx context.Context, repos Repos) error) error
}
