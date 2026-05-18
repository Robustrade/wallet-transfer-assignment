package repository

import (
	"context"

	"github.com/wallet-transfer-assignment/internal/domain"
)

// Repository defines the data access operations for the wallet transfer system.
// Each method operates within the scope of a transaction when used via TxManager.
type Repository interface {
	// Wallet operations
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
	GetWalletForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	DebitWallet(ctx context.Context, id string, amount int64) error
	CreditWallet(ctx context.Context, id string, amount int64) error

	// Transfer operations
	CreateTransfer(ctx context.Context, transfer *domain.Transfer) error
	UpdateTransferStatus(ctx context.Context, id string, status domain.TransferStatus) error
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)

	// Ledger operations
	CreateLedgerEntry(ctx context.Context, entry *domain.LedgerEntry) error
	GetLedgerEntriesByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)

	// Idempotency operations
	GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	CreateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error
}

// TxManager provides transactional execution. The callback receives a
// transaction-scoped Repository; all operations within the callback
// share the same database transaction.
type TxManager interface {
	WithTransaction(ctx context.Context, fn func(repo Repository) error) error
}
