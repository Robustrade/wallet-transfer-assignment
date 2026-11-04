package service

import (
	"context"

	"wallet-transfer/internal/domain"
)

type TransferService interface {
	CreateTransfer(ctx context.Context, input CreateTransferInput) (*domain.Transfer, error)
}

type TransferServiceImpl struct {
	transferRepo TransferRepository
}

func NewTransferService(transferRepo TransferRepository) TransferService {
	return &TransferServiceImpl{transferRepo: transferRepo}
}

type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

func (s *TransferServiceImpl) CreateTransfer(
	ctx context.Context,
	input CreateTransferInput,
) (*domain.Transfer, error) {
	// TODO: Implement the transfer use case.
	return nil, domain.ErrNotImplemented
}
