package service

import (
	"context"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
)

// The interfaces in this file are the ports the service depends on. Adapters
// (the PostgreSQL repositories, in-memory fakes for tests) implement them. The
// service owns these definitions so that business logic dictates the contract,
// not the persistence technology.
//
// Repository methods participate in the ambient transaction carried on the
// context by TxManager.WithinTx. Implementations resolve the active
// transaction from the context, falling back to the connection pool when none
// is present, so call sites never thread a *tx handle by hand.

// WalletRepository persists and locks wallet rows.
type WalletRepository interface {
	// Create inserts a new wallet.
	Create(ctx context.Context, wallet *domain.Wallet) error
	// Get returns a wallet without locking, or domain.ErrWalletNotFound.
	Get(ctx context.Context, id string) (*domain.Wallet, error)
	// GetForUpdate returns a wallet while acquiring a row-level write lock
	// (SELECT ... FOR UPDATE), serializing concurrent debits/credits on that
	// wallet. Returns domain.ErrWalletNotFound when the wallet is absent.
	GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	// UpdateBalance persists a new balance for an existing wallet.
	UpdateBalance(ctx context.Context, id string, balance domain.Money) error
}

// TransferRepository persists transfers and their lifecycle.
type TransferRepository interface {
	// Create inserts a new transfer.
	Create(ctx context.Context, transfer *domain.Transfer) error
	// Get returns a transfer by id, or domain.ErrTransferNotFound.
	Get(ctx context.Context, id string) (*domain.Transfer, error)
	// Update persists status and failure-reason changes for a transfer.
	Update(ctx context.Context, transfer *domain.Transfer) error
}

// LedgerRepository persists double-entry ledger rows.
type LedgerRepository interface {
	// CreateEntries inserts ledger entries atomically (the balanced pair for a
	// transfer is written together).
	CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error
	// ListByWallet returns the ledger history for a wallet, newest first.
	ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

// IdempotencyRepository is the durable dedup store.
type IdempotencyRepository interface {
	// Insert claims an idempotency key. It returns
	// domain.ErrDuplicateIdempotencyKey if the key already exists; concurrent
	// callers block on the UNIQUE constraint until the first transaction
	// resolves.
	Insert(ctx context.Context, record *domain.IdempotencyRecord) error
	// Get returns the record for a key, or domain.ErrTransferNotFound-style
	// absence is signaled by a nil record with no error only when the caller
	// expects possible absence; otherwise it returns a not-found error.
	Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	// Complete finalizes a record with the produced transfer id and the
	// serialized response to replay for future duplicates.
	Complete(ctx context.Context, key, transferID string, response []byte) error
}

// TxManager defines the transactional boundary for the service. WithinTx runs
// fn inside a single database transaction, committing on a nil error and
// rolling back otherwise. The transaction is propagated to repositories via the
// context passed to fn.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Repositories bundles the ports the service depends on. It is the single
// wiring point assembled in main.go (with PostgreSQL adapters) and in tests
// (with in-memory fakes), and passed to New.
type Repositories struct {
	Wallets     WalletRepository
	Transfers   TransferRepository
	Ledger      LedgerRepository
	Idempotency IdempotencyRepository
	TxManager   TxManager
}
