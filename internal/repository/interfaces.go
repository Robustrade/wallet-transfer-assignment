package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// Querier is the minimal interface that both *pgxpool.Pool and pgx.Tx satisfy,
// allowing repositories to work in or out of a transaction.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// WalletRepository defines persistence operations for wallets.
type WalletRepository interface {
	// Create inserts a new wallet and returns it.
	Create(ctx context.Context, q Querier, w domain.Wallet) error
	// GetByIDForUpdate reads a wallet by ID and acquires a row-level lock
	// (SELECT … FOR UPDATE) within the given transaction.
	GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (domain.Wallet, error)
	// GetByID reads a wallet by ID without locking.
	GetByID(ctx context.Context, q Querier, id uuid.UUID) (domain.Wallet, error)
	// UpdateBalance sets the wallet balance and increments version.
	UpdateBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, newBalance domain.Amount) error
}

// TransferRepository defines persistence operations for transfers.
type TransferRepository interface {
	// Create inserts a new transfer record.
	Create(ctx context.Context, q Querier, t domain.Transfer) error
	// GetByID retrieves a transfer by its primary key.
	GetByID(ctx context.Context, q Querier, id uuid.UUID) (domain.Transfer, error)
	// UpdateStatus transitions a transfer to the given status.
	UpdateStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status domain.TransferStatus) error
}

// LedgerRepository defines persistence operations for ledger entries.
type LedgerRepository interface {
	// CreateEntries inserts two ledger entries within a transaction.
	CreateEntries(ctx context.Context, tx pgx.Tx, debit, credit domain.LedgerEntry) error
}

// IdempotencyRepository defines persistence operations for idempotency records.
type IdempotencyRepository interface {
	// Get returns the cached record for the given key, or domain.ErrTransferNotFound.
	Get(ctx context.Context, q Querier, key string) (IdempotencyRecord, error)
	// Create stores a new idempotency record within a transaction.
	Create(ctx context.Context, tx pgx.Tx, r IdempotencyRecord) error
}

// IdempotencyRecord is the persistence representation of a cached API response.
type IdempotencyRecord struct {
	Key          string
	TransferID   uuid.UUID
	ResponseJSON string
	StatusCode   int
	// RequestHash is a SHA-256 fingerprint of the original request parameters
	// (fromWalletID, toWalletID, amount). It is used to detect key reuse with
	// conflicting parameters.
	RequestHash string
}
