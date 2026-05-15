package dto

import "github.com/google/uuid"

type CreateTransferRequest struct {
	IdempotencyKey string    `json:"idempotency_key"`
	FromWalletID   uuid.UUID `json:"from_wallet_id" validate:"required"`
	ToWalletID     uuid.UUID `json:"to_wallet_id" validate:"required"`
	Amount         int64     `json:"amount" validate:"required,gt=0"`
}

type TransferResponse struct {
	TransferID string `json:"transfer_id"`
	Status     string `json:"transfer_status"`
}
