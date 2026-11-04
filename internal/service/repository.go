package service

import (
	"context"

	"wallet-transfer/internal/domain"
)

type TransferRepository interface {
	CreateTransfer(ctx context.Context, transfer *domain.Transfer) error
	GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error)
}
