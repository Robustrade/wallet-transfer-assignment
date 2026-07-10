package domain

import "time"

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         float64
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
