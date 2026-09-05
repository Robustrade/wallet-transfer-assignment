package service

import (
	"context"

	"github.com/google/uuid"

	"wallet-transfer/internal/domain"
)

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type WalletRepository interface {
	Get(ctx context.Context, id string) (domain.Wallet, error)
	LockPair(ctx context.Context, firstID, secondID string) (domain.Wallet, domain.Wallet, error)
	UpdateBalance(ctx context.Context, id string, balance domain.Amount) error
}

type TransferRepository interface {
	Create(ctx context.Context, t *domain.Transfer) error
	Get(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
	ListByWallet(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, from, to domain.TransferStatus, reason *string) error
}

type LedgerRepository interface {
	CreatePair(ctx context.Context, entries [2]domain.LedgerEntry) error
	ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error)
}

type IdempotencyRepository interface {
	Get(ctx context.Context, key string) (domain.IdempotencyRecord, error)
	Create(ctx context.Context, record domain.IdempotencyRecord) error
}
