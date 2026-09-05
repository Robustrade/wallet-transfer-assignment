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
	LockPair(ctx context.Context, firstID, secondID string) (domain.Wallet, domain.Wallet, error)
	UpdateBalance(ctx context.Context, id string, balance domain.Amount) error
}

type TransferRepository interface {
	Create(ctx context.Context, t *domain.Transfer) error
	UpdateStatus(ctx context.Context, id uuid.UUID, from, to domain.TransferStatus, reason *string) error
}

type LedgerRepository interface {
	CreatePair(ctx context.Context, entries [2]domain.LedgerEntry) error
}

type IdempotencyRepository interface {
	Get(ctx context.Context, key string) (domain.IdempotencyRecord, error)
	Create(ctx context.Context, record domain.IdempotencyRecord) error
}
