package domain

import (
	"errors"
	"fmt"
	"time"
)

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

var (
	ErrSameWallet        = errors.New("sender and receiver cannot be the same")
	ErrInvalidTransition = errors.New("invalid transfer status transition")
)

type Transfer struct {
	ID             int64
	IdempotencyKey string
	FromWalletID   int64
	ToWalletID     int64
	Amount         int64
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (t Transfer) Validate() error {
	if t.Amount <= 0 {
		return ErrInvalidAmount
	}

	if t.FromWalletID <= 0 || t.ToWalletID <= 0 {
		return ErrWalletNotFound
	}

	if t.FromWalletID == t.ToWalletID {
		return ErrSameWallet
	}

	if len(t.IdempotencyKey) < MinIdempotencyKeyLength || len(t.IdempotencyKey) > MaxIdempotencyKeyLength {
		return fmt.Errorf("idempotency key must be between %d and %d characters", MinIdempotencyKeyLength, MaxIdempotencyKeyLength)
	}

	return nil
}

func (t *Transfer) ChangeStatus(status TransferStatus) error {
	if t.Status != TransferPending {
		return ErrInvalidTransition
	}

	if status != TransferProcessed && status != TransferFailed {
		return ErrInvalidTransition
	}

	t.Status = status
	return nil
}
