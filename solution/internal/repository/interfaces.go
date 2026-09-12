package repository

import (
	"context"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
)

// These interfaces are deliberately consumer-shaped: they carry only what
// TransferService actually calls, so the fakes in its tests stay small.

type WalletRepository interface {
	Get(ctx context.Context, id string) (*domain.Wallet, error)
}

type TransferRepository interface {
	Create(ctx context.Context, t *domain.Transfer) error
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
}

// TransferExecutor atomically debits, credits, writes the ledger
// entries, and marks the transfer PROCESSED.
type TransferExecutor interface {
	Execute(ctx context.Context, transfer *domain.Transfer, debit, credit domain.LedgerEntry) error
}
