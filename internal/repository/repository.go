package repository

import (
	"context"
	"database/sql"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
)

// Repository specifies the persistence contract for wallets, transfers, ledger entries, and idempotency records.
type Repository interface {
	// Wallet operations
	CreateWallet(ctx context.Context, wallet *model.Wallet) error
	GetWallet(ctx context.Context, id string) (*model.Wallet, error)
	// GetWalletsForUpdate locks both wallets in deterministic lexicographical order to eliminate deadlocks.
	GetWalletsForUpdate(ctx context.Context, tx *sql.Tx, id1, id2 string) (w1 *model.Wallet, w2 *model.Wallet, err error)
	UpdateWalletBalance(ctx context.Context, tx *sql.Tx, id string, newBalance int64) error

	// Transfer operations
	CreateTransfer(ctx context.Context, tx *sql.Tx, transfer *model.Transfer) error
	GetTransfer(ctx context.Context, id string) (*model.Transfer, error)
	GetTransferByIdempotencyKey(ctx context.Context, key string) (*model.Transfer, error)

	// Ledger operations (Append-Only)
	CreateLedgerEntries(ctx context.Context, tx *sql.Tx, entries []model.LedgerEntry) error
	GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]model.LedgerEntry, error)
	GetLedgerEntriesByWalletID(ctx context.Context, walletID string) ([]model.LedgerEntry, error)
	GetLedgerBalance(ctx context.Context, walletID string) (int64, error)

	// Idempotency operations
	GetIdempotencyRecord(ctx context.Context, key string) (*model.IdempotencyRecord, error)
	SaveIdempotencyRecord(ctx context.Context, tx *sql.Tx, record *model.IdempotencyRecord) error

	// Transaction management
	BeginTx(ctx context.Context) (*sql.Tx, error)
	ExecTx(ctx context.Context, fn func(tx *sql.Tx) error) error
	Close() error
}
