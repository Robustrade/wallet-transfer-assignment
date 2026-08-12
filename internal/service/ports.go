// Package service contains the business logic and transactional
// orchestration for wallet transfers. It depends only on the domain package
// and on the interfaces defined in this file — never on a concrete storage
// package — so it can be unit tested against fakes and swapped onto a
// different backing store without changing a single line of business logic.
package service

import (
	"context"
	"time"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/google/uuid"
)

// Repos groups the read/write access points the service needs. A Repos
// obtained outside a UnitOfWork.Do call performs reads without a
// transaction; a Repos passed into the fn given to Do operates within that
// transaction.
type Repos interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledger() LedgerRepository
}

// UnitOfWork runs fn inside a single database transaction, retrying the
// whole transaction on serialization failures and deadlocks. fn must have no
// side effects outside of the Repos it is given — retries re-run fn from
// scratch. UnitOfWork also exposes Repos directly for non-transactional
// reads (e.g. the idempotency replay fast path).
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, r Repos) error) error
	Repos
}

// WalletRepository persists and locks wallets.
type WalletRepository interface {
	// Get reads a wallet without locking it.
	Get(ctx context.Context, id string) (domain.Wallet, error)
	// GetForUpdate reads a wallet under a FOR NO KEY UPDATE lock. Must be
	// called within a transaction.
	GetForUpdate(ctx context.Context, id string) (domain.Wallet, error)
	// Create inserts a new wallet. Returns domain.ErrWalletAlreadyExists if
	// the id is taken.
	Create(ctx context.Context, w domain.Wallet) error
	// ApplyDelta atomically adds delta (negative for a debit) to the
	// wallet's balance via a guarded UPDATE, returning the resulting
	// balance. Returns domain.ErrInsufficientFunds if the guard rejects the
	// update, or domain.ErrWalletNotFound if the wallet does not exist.
	ApplyDelta(ctx context.Context, id string, delta int64) (newBalance int64, err error)
}

// TransferRepository persists transfers and enforces the idempotency
// dedup mechanism described in docs/design.md §6.
type TransferRepository interface {
	// InsertIfAbsent attempts to claim t.IdempotencyKey. inserted is false
	// if the key was already claimed (by a committed or concurrently
	// in-flight transfer); callers must then look the existing transfer up
	// with FindByIdempotencyKey.
	InsertIfAbsent(ctx context.Context, t domain.Transfer) (inserted bool, err error)
	FindByID(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
	FindByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error)
	// MarkProcessed transitions a PENDING transfer to PROCESSED. It is a
	// guarded UPDATE ... WHERE status = 'PENDING'; the caller must treat
	// zero rows affected as an invariant violation.
	MarkProcessed(ctx context.Context, id uuid.UUID, settledAt time.Time) error
	// MarkFailed transitions a PENDING transfer to FAILED with the given
	// reason. Same guard as MarkProcessed.
	MarkFailed(ctx context.Context, id uuid.UUID, code domain.FailureCode, message string) error
}

// LedgerRepository persists and reads the append-only ledger.
type LedgerRepository interface {
	// InsertPair writes both halves of a double-entry in one statement
	// batch. Both rows must belong to the same transfer.
	InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error
	// ListByWallet returns entries newest-first, keyset-paginated on id.
	// beforeID nil means "start from the newest entry".
	ListByWallet(ctx context.Context, walletID string, limit int, beforeID *int64) ([]domain.LedgerEntry, error)
}
