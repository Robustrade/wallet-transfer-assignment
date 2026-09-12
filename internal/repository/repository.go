package repository

import (
	"context"
	"time"

	"wallet-transfer-assignment/internal/domain"
)

// WalletRepository manages storage operations for wallets.
type WalletRepository interface {
	CreateWallet(ctx context.Context, wallet *domain.Wallet) error
	GetWalletByID(ctx context.Context, id string) (*domain.Wallet, error)
	GetWalletByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	UpdateWalletBalance(ctx context.Context, id string, newBalance int64) error
}

// TransferRepository manages storage operations for transfers.
type TransferRepository interface {
	CreateTransfer(ctx context.Context, transfer *domain.Transfer) error
	GetTransferByID(ctx context.Context, id string) (*domain.Transfer, error)
	GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
	UpdateTransferStatus(ctx context.Context, id string, status domain.TransferStatus, failureReason *string) error
}

// LedgerRepository manages append-only double-entry ledger records.
type LedgerRepository interface {
	CreateLedgerEntries(ctx context.Context, entries ...domain.LedgerEntry) error
	GetLedgerByWalletID(ctx context.Context, walletID string, limit, offset int) ([]domain.LedgerEntry, error)
	GetLedgerByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
	CalculateLedgerBalance(ctx context.Context, walletID string) (int64, error)
}

// IdempotencyRepository manages idempotency records and response caching.
type IdempotencyRepository interface {
	ReserveIdempotency(ctx context.Context, record *domain.IdempotencyRecord, staleTimeout time.Duration) (*domain.IdempotencyRecord, bool, error)
	GetIdempotency(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	UpdateIdempotency(ctx context.Context, record *domain.IdempotencyRecord) error
	DeleteInProgress(ctx context.Context, key string, ownerToken string) error
	DeleteStaleInProgress(ctx context.Context, key string, ownerToken string, maxUpdatedAt time.Time) error
	HeartbeatIdempotency(ctx context.Context, key string, ownerToken string) error
}

// Repositories aggregates all repository interfaces.
type Repositories struct {
	Wallets     WalletRepository
	Transfers   TransferRepository
	Ledger      LedgerRepository
	Idempotency IdempotencyRepository
}

// TxManager defines the contract for executing business logic within an atomic transaction.
type TxManager interface {
	ExecuteInTx(ctx context.Context, fn func(txRepos Repositories) error) error
}
