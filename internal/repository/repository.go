package repository

import (
	"context"

	"wallet-service/internal/domain"
)

type TransferResult struct {
	Transfer *domain.Transfer
	Error    error
}

type Repository interface {
	// ExecuteTransfer executes a wallet transfer safely in a transaction.
	// It should check the idempotency key, verify balances, insert ledger entries, and update balances.
	ExecuteTransfer(ctx context.Context, transferReq *domain.Transfer) (*domain.Transfer, error)

	GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
}
