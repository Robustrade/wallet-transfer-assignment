package repository

import (
	"context"
	"database/sql"

	"github.com/Saket121/wallet-transfer-assignment/internal/domain"
)

// TransferRepository defines the persistence interface for transfers.
type TransferRepository interface {
	// FindTransferByIdempotencyKey looks up an existing transfer by its idempotency key.
	FindTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)

	// ExecuteTransfer performs the full transfer within a single transaction:
	// insert transfer, lock wallets, update balances, insert ledger entries, update status.
	ExecuteTransfer(ctx context.Context, transfer *domain.Transfer) (*domain.Transfer, error)
}

// WalletRepository defines the persistence interface for wallets.
type WalletRepository interface {
	// GetWallet retrieves a wallet by ID.
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
}

// TxFunc is a function executed within a database transaction.
type TxFunc func(tx *sql.Tx) error
