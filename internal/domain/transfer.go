package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID           uuid.UUID
	FromWalletID uuid.UUID
	ToWalletID   uuid.UUID
	Amount       int64
	Status       TransferStatus
	CreatedAt    time.Time
}
