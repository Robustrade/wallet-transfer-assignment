package service

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
)

type CreateTransferRequest struct {
	FromWalletId   *string `json:"fromWalletId"`
	ToWalletId     *string `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	IdempotencyKey string  `json:"idempotencyKey"`
}
type TransferRepository interface {
	Create(ctx context.Context, transfer *domain.Transfer) (*domain.Transfer, error)
}

type TransferService struct {
	TransferRepository TransferRepository
}

func NewTransferService(TransferRepository TransferRepository) *TransferService {
	return &TransferService{TransferRepository: TransferRepository}
}

func (t *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*domain.Transfer, error) {
	transfer := &domain.Transfer{}

	return t.TransferRepository.Create(ctx, transfer)

}
