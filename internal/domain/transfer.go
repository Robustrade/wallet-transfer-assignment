package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransferFailureReason string

const (
	TransferFailureNone TransferFailureReason = "NONE"

	TransferFailureInsufficientFunds TransferFailureReason = "INSUFFICIENT_FUNDS"

	TransferFailureWalletNotFound TransferFailureReason = "WALLET_NOT_FOUND"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type CreateTransfer struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
}

type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
	Status         TransferStatus
	FailureReason  TransferFailureReason
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
