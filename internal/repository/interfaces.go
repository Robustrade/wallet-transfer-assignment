package repository

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
)

// WalletRepository defines persistence operations for wallets.
type WalletRepository interface {
	// Create inserts a new wallet.
	Create(ctx context.Context, tx pgx.Tx, wallet *domain.Wallet) error

	// GetByID retrieves a wallet by its ID.
	GetByID(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error)

	// GetByIDForUpdate retrieves a wallet by its ID with a row-level lock (SELECT FOR UPDATE).
	GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error)

	// UpdateBalance updates the balance of a wallet.
	UpdateBalance(ctx context.Context, tx pgx.Tx, id string, newBalance int64) error
}

// TransferRepository defines persistence operations for transfers.
type TransferRepository interface {
	// Create inserts a new transfer. Returns the existing transfer if the idempotency key already exists.
	Create(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) (*domain.Transfer, bool, error)

	// GetByID retrieves a transfer by its ID.
	GetByID(ctx context.Context, tx pgx.Tx, id string) (*domain.Transfer, error)

	// GetByIdempotencyKey retrieves a transfer by its idempotency key.
	GetByIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Transfer, error)

	// UpdateStatus updates the status (and optional failure reason) of a transfer.
	UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status domain.TransferStatus, failureReason string) error
}

// LedgerRepository defines persistence operations for ledger entries.
type LedgerRepository interface {
	// CreateEntries inserts a pair of ledger entries (debit + credit) atomically.
	CreateEntries(ctx context.Context, tx pgx.Tx, entries []*domain.LedgerEntry) error

	// GetByTransferID retrieves all ledger entries for a transfer.
	GetByTransferID(ctx context.Context, tx pgx.Tx, transferID string) ([]*domain.LedgerEntry, error)

	// GetByWalletID retrieves all ledger entries for a wallet, ordered by creation time.
	GetByWalletID(ctx context.Context, tx pgx.Tx, walletID string, limit, offset int) ([]*domain.LedgerEntry, error)
}

// TxManager manages database transactions.
type TxManager interface {
	// WithTx executes a function within a database transaction.
	// The transaction is committed if the function returns nil, rolled back otherwise.
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}
