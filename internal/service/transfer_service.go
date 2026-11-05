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
	if input.IdempotencyKey == "" || input.FromWalletID == "" || input.ToWalletID == "" ||
		input.FromWalletID == input.ToWalletID || input.Amount <= 0 {
		return nil, domain.ErrInvalidTransfer
	}

	transfer := &domain.Transfer{
		IdempotencyKey: input.IdempotencyKey,
		FromWalletID:   input.FromWalletID,
		ToWalletID:     input.ToWalletID,
		Amount:         input.Amount,
	}

	// The repository owns idempotency lookup and execution in one write
	// transaction. Do not replay RETRYABLE_FAILED here: it must re-check funds.

	return s.transferRepo.TransferFunds(ctx, transfer)
}
