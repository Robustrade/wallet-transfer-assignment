package service

import (
	"context"

	"wallet-transfer/internal/domain"
)

type TransferRepository interface {
	TransferFunds(ctx context.Context, transfer *domain.Transfer) (*domain.Transfer, error)
}
