package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

var (
	ErrInvalidIdempotencyKey = errors.New("idempotencyKey is required")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrSameWallet            = errors.New("fromWalletId and toWalletId must differ")
	ErrInvalidWalletID       = errors.New("wallet id is required")
	ErrInvalidTransferStatus = errors.New("invalid transfer status")
)

type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return ErrInvalidIdempotencyKey
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return ErrInvalidWalletID
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}

func (s TransferStatus) IsValid() bool {
	switch s {
	case TransferStatusPending, TransferStatusProcessed, TransferStatusFailed:
		return true
	default:
		return false
	}
}

func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	if !next.IsValid() {
		return false
	}

	switch s {
	case TransferStatusPending:
		return next == TransferStatusProcessed || next == TransferStatusFailed
	case TransferStatusProcessed, TransferStatusFailed:
		return false
	default:
		return false
	}
}

func (s TransferStatus) String() string {
	return string(s)
}

func ParseTransferStatus(value string) (TransferStatus, error) {
	status := TransferStatus(value)
	if !status.IsValid() {
		return "", fmt.Errorf("%w: %s", ErrInvalidTransferStatus, value)
	}
	return status, nil
}
