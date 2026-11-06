// Package service implements the transfer workflow: idempotency, transaction
// boundaries, locking order and orchestration of the domain rules.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// The interfaces below are the ports this layer needs from persistence. They are
// implemented by internal/repository/postgres. Repositories only read and write;
// every workflow decision is made here or in the domain.

// Store gives access to the repositories. A Store passed to InTx is bound to
// that transaction.
type Store interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledger() LedgerRepository
	Idempotency() IdempotencyRepository
}

// Database is a Store that can also open transactions.
type Database interface {
	Store
	// InTx runs fn in a single READ COMMITTED transaction. The transaction commits if
	// fn returns nil and rolls back otherwise (including on panic). Lock waits are
	// bounded; exceeding the bound yields domain.ErrBusy.
	InTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
}

// WalletReader reads wallets. Read-only consumers depend on this alone.
type WalletReader interface {
	// Get returns the wallet or domain.ErrWalletNotFound.
	Get(ctx context.Context, id string) (domain.Wallet, error)
}

// WalletRepository reads, locks and moves money on wallets.
type WalletRepository interface {
	WalletReader
	// GetForUpdate returns the wallet and row-locks it until the transaction ends.
	GetForUpdate(ctx context.Context, id string) (domain.Wallet, error)
	// Debit subtracts a positive amount from the wallet balance.
	Debit(ctx context.Context, id string, amount int64) error
	// Credit adds a positive amount to the wallet balance.
	Credit(ctx context.Context, id string, amount int64) error
}

// TransferRepository persists transfers.
type TransferRepository interface {
	Create(ctx context.Context, t domain.Transfer) error
	// UpdateStatus persists t's status, failure reason and timestamp, but only if the
	// stored status is still from; otherwise it returns domain.ErrInvalidTransition.
	UpdateStatus(ctx context.Context, t domain.Transfer, from domain.TransferStatus) error
	// Get returns the transfer or domain.ErrTransferNotFound.
	Get(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
}

// LedgerRepository appends immutable ledger entries.
type LedgerRepository interface {
	Append(ctx context.Context, entries []domain.LedgerEntry) error
}

// IdempotencyRecord binds a client idempotency key to a request fingerprint and
// the transfer created for it.
type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  uuid.UUID
	CreatedAt   time.Time
}

// IdempotencyRepository persists idempotency records.
type IdempotencyRepository interface {
	// Claim inserts rec unless its key already exists and reports whether it did.
	// While another uncommitted transaction holds the same key, Claim blocks until
	// that transaction ends.
	Claim(ctx context.Context, rec IdempotencyRecord) (bool, error)
	// Get returns the committed record for key.
	Get(ctx context.Context, key string) (IdempotencyRecord, error)
}
