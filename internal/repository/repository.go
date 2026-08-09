package repository

import (
	"context"
	"database/sql"

	"github.com/shanks/wallet-transfer-assignment/internal/domain"
)

// TxManager abstracts transaction boundaries.
type TxManager interface {
	ExecTx(ctx context.Context, fn func(tx *sql.Tx) error) error
}

// WalletRepository defines persistence operations for wallets.
type WalletRepository interface {
	Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error
	GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error)
	GetByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error)
	GetTwoWalletsForUpdate(ctx context.Context, tx *sql.Tx, id1, id2 string) (*domain.Wallet, *domain.Wallet, error)
	UpdateBalance(ctx context.Context, tx *sql.Tx, id string, newBalance int64) error
	CalculateLedgerBalance(ctx context.Context, tx *sql.Tx, walletID string) (int64, error)
}

// TransferRepository defines persistence operations for transfers.
type TransferRepository interface {
	Create(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer) error
	GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Transfer, error)
	GetByIdempotencyKey(ctx context.Context, tx *sql.Tx, key string) (*domain.Transfer, error)
	UpdateStatus(ctx context.Context, tx *sql.Tx, id string, status domain.TransferStatus, failureReason string) error
}

// LedgerRepository defines persistence operations for double-entry ledger records.
type LedgerRepository interface {
	CreateEntries(ctx context.Context, tx *sql.Tx, entries []domain.LedgerEntry) error
	GetByTransferID(ctx context.Context, tx *sql.Tx, transferID string) ([]domain.LedgerEntry, error)
	GetByWalletID(ctx context.Context, tx *sql.Tx, walletID string) ([]domain.LedgerEntry, error)
}

// IdempotencyRepository defines persistence operations for idempotency records.
type IdempotencyRepository interface {
	Create(ctx context.Context, tx *sql.Tx, rec *domain.IdempotencyRecord) error
	Get(ctx context.Context, tx *sql.Tx, key string) (*domain.IdempotencyRecord, error)
	GetForUpdate(ctx context.Context, tx *sql.Tx, key string) (*domain.IdempotencyRecord, error)
	Update(ctx context.Context, tx *sql.Tx, rec *domain.IdempotencyRecord) error
}
