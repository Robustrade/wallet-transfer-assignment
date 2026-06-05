package service

import "github.com/google/uuid"

type CreateTransferRequest struct {
	IdempotencyKey string

	FromWalletID uuid.UUID
	ToWalletID   uuid.UUID

	Amount int64
}
