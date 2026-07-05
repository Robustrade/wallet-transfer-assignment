package repository

import (
	"context"
	"database/sql"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
)

// Querier is satisfied by both *sql.DB and *sql.Tx, allowing repository
// methods to run either standalone or within a transaction the service
// layer controls.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type WalletRepository interface {
	GetForUpdate(ctx context.Context, q Querier, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, q Querier, id string, newBalance int64) error
	Get(ctx context.Context, q Querier, id string) (*domain.Wallet, error)
}

type TransferRepository interface {
	Create(ctx context.Context, q Querier, t *domain.Transfer) error
	UpdateStatus(ctx context.Context, q Querier, id string, status domain.TransferStatus, failureReason string) error
	GetByID(ctx context.Context, q Querier, id string) (*domain.Transfer, error)
	ListByWallet(ctx context.Context, q Querier, walletID string, limit, offset int) ([]*domain.Transfer, error)
}

type LedgerRepository interface {
	InsertPair(ctx context.Context, q Querier, debit, credit *domain.LedgerEntry) error
}

type IdempotencyRepository interface {
	Insert(ctx context.Context, q Querier, key, fingerprint string) (*domain.IdempotencyRecord, error)
	GetByKey(ctx context.Context, q Querier, key string) (*domain.IdempotencyRecord, error)
	Complete(ctx context.Context, q Querier, key string, transferID string, responseSnapshot []byte) error
}

// ErrNotFound is returned by repository methods when a row doesn't exist.
var ErrNotFound = sql.ErrNoRows
