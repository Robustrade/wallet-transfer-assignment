package repository

import (
	"context"

	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/domain"
)

// UnitOfWork executes fn in one database transaction. Returning an error rolls
// the transaction back; returning nil commits it.
type UnitOfWork interface {
	WithinTransaction(ctx context.Context, fn func(Transaction) error) error
}

type Transaction interface {
	TryCreateIdempotency(ctx context.Context, key, requestHash string) (bool, error)
	FindTransferByIdempotencyKey(ctx context.Context, key string) (*domain.IdempotencyRecord, *domain.Transfer, error)
	LinkIdempotencyToTransfer(ctx context.Context, key, transferID string) error
	CompleteIdempotency(ctx context.Context, key string) error

	CreateTransfer(ctx context.Context, transfer *domain.Transfer) error
	UpdateTransfer(ctx context.Context, transfer *domain.Transfer) error

	LockWallets(ctx context.Context, walletIDs []string) ([]domain.Wallet, error)
	UpdateWalletBalance(ctx context.Context, walletID string, balance int64) error

	CreateLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error
}
