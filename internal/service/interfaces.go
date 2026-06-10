package service

import (
	"context"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/repository"
)

// WalletRepo defines the persistence operations required for wallets.
type WalletRepo interface {
	Create(ctx context.Context, db repository.DBTX, wallet *domain.Wallet) (*domain.Wallet, error)
	GetByID(ctx context.Context, db repository.DBTX, id string) (*domain.Wallet, error)
	GetByIDForUpdate(ctx context.Context, db repository.DBTX, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, db repository.DBTX, id string, delta int64) error
}

// TransferRepo defines the persistence operations required for transfers.
type TransferRepo interface {
	Create(ctx context.Context, db repository.DBTX, transfer *domain.Transfer) (*domain.Transfer, error)
	GetByID(ctx context.Context, db repository.DBTX, id string) (*domain.Transfer, error)
	UpdateStatus(ctx context.Context, db repository.DBTX, id string, status domain.TransferStatus, errorReason *string) error
}

// LedgerRepo defines the persistence operations required for ledger entries.
type LedgerRepo interface {
	CreatePair(ctx context.Context, db repository.DBTX, transferID, fromWalletID, toWalletID string, amount int64) error
	GetByTransferID(ctx context.Context, db repository.DBTX, transferID string) ([]domain.LedgerEntry, error)
	GetByWalletID(ctx context.Context, db repository.DBTX, walletID string) ([]domain.LedgerEntry, error)
}

// IdempotencyRepo defines the persistence operations required for idempotency records.
type IdempotencyRepo interface {
	Create(ctx context.Context, db repository.DBTX, record *domain.IdempotencyRecord) (bool, error)
	GetByKey(ctx context.Context, db repository.DBTX, key string) (*domain.IdempotencyRecord, error)
	MarkCompleted(ctx context.Context, db repository.DBTX, key string, transferID string) error
	MarkFailed(ctx context.Context, db repository.DBTX, key string, transferID *string) error
	TryAdvisoryXactLock(ctx context.Context, db repository.DBTX, key string) (bool, error)
}
