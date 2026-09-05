package dto

import (
	"github.com/google/uuid"

	"wallet-transfer-assignment/internal/domain"
)

type CreateTransferRequest struct {
	IdempotencyKey string    `json:"idempotencyKey" validate:"required"`
	FromWalletID   uuid.UUID `json:"fromWalletId" validate:"required"`
	ToWalletID     uuid.UUID `json:"toWalletId" validate:"required"`
	Amount         int64     `json:"amount" validate:"required,gt=0"`
}

func (r CreateTransferRequest) ToDomain() domain.CreateTransfer {
	return domain.CreateTransfer{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         r.Amount,
	}
}
