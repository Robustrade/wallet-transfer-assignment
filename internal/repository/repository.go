package repository

import (
	"context"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

// WalletRepository persists wallet rows. Use LockByID inside a transaction for pessimistic locking.
type WalletRepository interface {
	GetByID(ctx context.Context, walletID string) (*domain.Wallet, error)
	LockByID(ctx context.Context, walletID string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, wallet *domain.Wallet) error
}

// TransferRepository persists transfer rows and state.
type TransferRepository interface {
	Create(ctx context.Context, transfer *domain.Transfer) error
	GetByID(ctx context.Context, transferID string) (*domain.Transfer, error)
	UpdateState(ctx context.Context, transferID string, state domain.TransferState) error
}

// LedgerRepository persists double-entry ledger rows.
type LedgerRepository interface {
	Create(ctx context.Context, entry *domain.LedgerEntry) error
	ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

// IdempotencyRepository stores and retrieves idempotency records.
type IdempotencyRepository interface {
	GetByKey(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	Save(ctx context.Context, record *domain.IdempotencyRecord) error
}

// TxManager runs fn inside a database transaction.
type TxManager interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// Store groups repositories used by the service layer.
type Store interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledger() LedgerRepository
	Idempotency() IdempotencyRepository
	Tx() TxManager
}
