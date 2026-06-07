// Package repository defines the persistence ports (interfaces) used by the
// service layer. Concrete implementations live in sub-packages (postgres,
// memory). Depending on these interfaces rather than a concrete database keeps
// the service layer testable and swappable (Dependency Inversion).
package repository

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// WalletRepository persists wallets and exposes the row-locking read required
// for safe concurrent debits.
type WalletRepository interface {
	// Create inserts a new wallet. It returns domain.ErrWalletAlreadyExists if
	// a wallet with the same id already exists.
	Create(ctx context.Context, w *domain.Wallet) error

	// Get reads a wallet without locking. Returns domain.ErrWalletNotFound when
	// absent.
	Get(ctx context.Context, id string) (*domain.Wallet, error)

	// GetForUpdate reads a wallet and acquires a row-level lock for the
	// duration of the surrounding transaction (SELECT ... FOR UPDATE). This is
	// the primitive that serialises concurrent debits on the same wallet.
	GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error)

	// UpdateBalance persists a new balance for an existing wallet.
	UpdateBalance(ctx context.Context, id string, newBalance int64) error
}

// TransferRepository persists transfers and provides the idempotent insert that
// underpins exactly-once semantics.
type TransferRepository interface {
	// InsertIfAbsent attempts to insert a PENDING transfer. The unique
	// constraint on idempotency_key makes this the deduplication point:
	//   - inserted=true  -> this caller owns the new transfer and must process it.
	//   - inserted=false -> a transfer with the same key already exists; the
	//     stored transfer is returned so the original result can be replayed.
	InsertIfAbsent(ctx context.Context, t *domain.Transfer) (inserted bool, existing *domain.Transfer, err error)

	// Get reads a transfer by id. Returns domain.ErrTransferNotFound when absent.
	Get(ctx context.Context, id string) (*domain.Transfer, error)

	// UpdateStatus persists a status (and failure reason) change for a transfer.
	UpdateStatus(ctx context.Context, t *domain.Transfer) error
}

// LedgerRepository persists double-entry ledger lines.
type LedgerRepository interface {
	// InsertPair writes the balanced debit/credit pair for a processed
	// transfer atomically within the surrounding transaction.
	InsertPair(ctx context.Context, debit, credit *domain.LedgerEntry) error

	// ListByWallet returns the ledger history for a wallet, newest first.
	ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

// Repositories groups the per-aggregate repositories. When supplied by
// WithinTx, every method runs on the same underlying transaction.
type Repositories struct {
	Wallets   WalletRepository
	Transfers TransferRepository
	Ledger    LedgerRepository
}

// Store is the persistence entry point exposed to the service layer.
type Store interface {
	// WithinTx runs fn inside a single transaction. The transaction is
	// committed when fn returns nil and rolled back otherwise, guaranteeing
	// all-or-nothing semantics for the transfer workflow.
	WithinTx(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error

	// Reader returns repositories bound to the connection pool for
	// non-transactional reads (status, balance, history).
	Reader() Repositories

	// Close releases underlying resources.
	Close()
}
