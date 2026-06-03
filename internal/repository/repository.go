package repository

import (
	"context"
	"wallet-transfer/internal/domain"

	"gorm.io/gorm"
)

type WalletRepository interface {
	Get(ctx context.Context, db *gorm.DB, id string) (*domain.Wallet, error)
	GetWithLock(ctx context.Context, db *gorm.DB, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, db *gorm.DB, id string, amount int64) error
	Create(ctx context.Context, db *gorm.DB, wallet *domain.Wallet) error
}

type TransferRepository interface {
	Create(ctx context.Context, db *gorm.DB, transfer *domain.Transfer) error
	Get(ctx context.Context, db *gorm.DB, id string) (*domain.Transfer, error)
}

type LedgerRepository interface {
	Create(ctx context.Context, db *gorm.DB, entry *domain.LedgerEntry) error
	GetByTransferID(ctx context.Context, db *gorm.DB, transferID string) ([]domain.LedgerEntry, error)
}

type IdempotencyRepository interface {
	CreatePending(ctx context.Context, db *gorm.DB, key string) error
	Get(ctx context.Context, db *gorm.DB, key string) (*domain.IdempotencyRecord, error)
	Update(ctx context.Context, db *gorm.DB, record *domain.IdempotencyRecord) error
}
