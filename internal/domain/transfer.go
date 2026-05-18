package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewTransfer(idempotencyKey, fromWalletID, toWalletID string, amount int64) (*Transfer, error) {
	if idempotencyKey == "" {
		return nil, ErrMissingIdempotencyKey
	}

	if fromWalletID == "" || toWalletID == "" {
		return nil, ErrInvalidWalletID
	}

	if fromWalletID == toWalletID {
		return nil, ErrSameWalletTransfer
	}

	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	return &Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         TransferStatusPending,
	}, nil
}

func (t *Transfer) MarkProcessed() error {
	if t.Status != TransferStatusPending {
		return ErrInvalidTransferState
	}

	t.Status = TransferStatusProcessed
	return nil
}

func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != TransferStatusPending {
		return ErrInvalidTransferState
	}

	t.Status = TransferStatusFailed
	t.FailureReason = &reason
	return nil
}
