package repository

import (
	"context"

	"wallet-transfer-assignment/internal/domain"

	"gorm.io/gorm"
)

// Repository groups all repository interfaces together.
type Repository interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledgers() LedgerRepository
	Idempotency() IdempotencyRepository
	Transactor
}

// WalletRepository manages wallet persistence.
type WalletRepository interface {
	Create(ctx context.Context, tx *gorm.DB, wallet *domain.Wallet) error
	GetByID(ctx context.Context, id string) (*domain.Wallet, error)
	GetForUpdate(ctx context.Context, tx *gorm.DB, id string) (*domain.Wallet, error)
	// GetPairForUpdate locks two wallets in deterministic order to prevent deadlocks.
	GetPairForUpdate(ctx context.Context, tx *gorm.DB, fromID, toID string) (fromWallet *domain.Wallet, toWallet *domain.Wallet, err error)
	UpdateBalance(ctx context.Context, tx *gorm.DB, wallet *domain.Wallet) error
}

// TransferRepository manages transfer records.
type TransferRepository interface {
	Create(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error
	GetByID(ctx context.Context, id string) (*domain.Transfer, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
	Update(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error
}

// LedgerRepository manages immutable double-entry ledger records.
type LedgerRepository interface {
	CreateEntries(ctx context.Context, tx *gorm.DB, entries []*domain.LedgerEntry) error
	GetByTransferID(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error)
	GetByWalletID(ctx context.Context, walletID string) ([]*domain.LedgerEntry, error)
	GetCalculatedBalance(ctx context.Context, walletID string) (int64, error)
}

// IdempotencyRepository manages idempotency records and cached responses.
type IdempotencyRepository interface {
	Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	Create(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error
	Update(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) error
}

// Transactor handles atomic database transactions.
type Transactor interface {
	ExecuteInTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error
}
