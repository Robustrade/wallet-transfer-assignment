package repository

import (
	"context"

	"Wallet_Transfer_Service/internal/domain"
)

type WalletRepository interface {
	Create(ctx context.Context, w *domain.Wallet) error
	Get(ctx context.Context, id string) (*domain.Wallet, error)
	GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, id string, newBalance int64) error
}

type TransferRepository interface {
	Create(ctx context.Context, t *domain.Transfer) error
	UpdateState(ctx context.Context, t *domain.Transfer) error
	Get(ctx context.Context, id string) (*domain.Transfer, error)
}

type LedgerRepository interface {
	// InsertPair persists the DEBIT/CREDIT pair produced by a single
	// transfer as one atomic unit.
	InsertPair(ctx context.Context, debit, credit *domain.LedgerEntry) error
	ListByTransfer(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error)
}

type IdempotencyRepository interface {
	Claim(ctx context.Context, key, requestHash string) (claimed bool, existing *domain.IdempotencyRecord, err error)
	// Complete records the final outcome for a previously claimed key.
	Complete(ctx context.Context, key, transferID string, responseStatus int, responseBody []byte) error
	Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
}

type Store interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledger() LedgerRepository
	Idempotency() IdempotencyRepository

	ExecTx(ctx context.Context, fn func(s Store) error) error
}
