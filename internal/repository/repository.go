// Package repository defines the persistence operations the service layer needs.
// Implementations do SQL only; the order of calls (the workflow) belongs to the service.
package repository

import (
	"context"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
)

// Repository is the set of operations available inside one transaction.
type Repository interface {
	// LockWallets locks the existing wallets among ids, in ascending id order,
	// and returns them. Missing ids are simply absent from the result.
	LockWallets(ctx context.Context, ids ...string) ([]domain.Wallet, error)

	// InsertTransfer inserts t unless its idempotency key is already taken. It reports
	// whether the row was inserted and, if so, sets t.CreatedAt. If another transaction
	// holds the same key uncommitted, it blocks until that transaction ends.
	InsertTransfer(ctx context.Context, t *domain.Transfer) (inserted bool, err error)

	// TransferByIdempotencyKey returns the transfer stored under key.
	TransferByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error)

	// UpdateTransferStatus persists t's status and failure reason. It only updates a
	// PENDING row; otherwise it returns domain.ErrInvalidTransition.
	UpdateTransferStatus(ctx context.Context, t domain.Transfer) error

	// AddToBalance adds delta (negative for a debit) to the wallet's balance.
	AddToBalance(ctx context.Context, walletID string, delta int64) error

	// InsertLedgerEntries appends the given entries.
	InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error
}

// TxManager runs fn in a single database transaction. It commits if fn returns nil
// and rolls back otherwise. Errors that are safe to retry come back wrapping
// domain.ErrTemporarilyUnavailable.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(repo Repository) error) error
}
