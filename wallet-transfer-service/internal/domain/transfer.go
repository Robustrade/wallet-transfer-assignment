package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID           uuid.UUID
	FromWalletID uuid.UUID
	ToWalletID   uuid.UUID
	Amount       int64 // smallest currency unit; e.g. paise
	Status       TransferStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
}
