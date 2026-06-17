package repository

import (
	"context"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/domain"
)

// TxManager runs fn inside a single DB transaction.
// Commits if fn returns nil, rolls back otherwise.
type TxManager interface {
	WithTx(ctx context.Context, fn func(repo Repository) error) error
}

// Repository holds persistence ops bound to one transaction.
type Repository interface {
	// Loads and row-locks a wallet (SELECT ... FOR UPDATE).
	GetWalletForUpdate(ctx context.Context, walletID string) (*domain.Wallet, error)

	// Sets a wallet balance.
	UpdateWalletBalance(ctx context.Context, walletID string, newBalance int64) error

	// Inserts a transfer and returns it with generated ID.
	CreateTransfer(ctx context.Context, t *domain.Transfer) (*domain.Transfer, error)

	// Updates a transfer's status.
	UpdateTransferStatus(ctx context.Context, transferID string, status domain.TransferStatus) error

	// Loads a transfer by ID.
	GetTransferByID(ctx context.Context, transferID string) (*domain.Transfer, error)

	// Inserts one ledger entry.
	CreateLedgerEntry(ctx context.Context, e *domain.LedgerEntry) error

	// Links an idempotency key to a transfer.
	CreateIdempotencyRecord(ctx context.Context, key string, transferID string) error

	// Resolves an idempotency key to its transfer ID.
	GetTransferIDByIdempotencyKey(ctx context.Context, key string) (string, error)
}