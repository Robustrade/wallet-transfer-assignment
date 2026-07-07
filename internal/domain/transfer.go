package domain

import "time"

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type FailureCode string

const (
	FailureCodeInsufficientBalance FailureCode = "INSUFFICIENT_BALANCE"
	FailureCodeWalletNotFound      FailureCode = "WALLET_NOT_FOUND"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureCode    FailureCode
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewPendingTransfer(
	id string,
	idempotencyKey string,
	fromWalletID string,
	toWalletID string,
	amount int64,
	now time.Time,
) *Transfer {
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         TransferStatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func (t *Transfer) MarkProcessed(now time.Time) error {
	if t.Status != TransferStatusPending {
		return ErrInvalidTransition
	}
	t.Status = TransferStatusProcessed
	t.FailureCode = ""
	t.UpdatedAt = now
	return nil
}

func (t *Transfer) MarkFailed(code FailureCode, now time.Time) error {
	if t.Status != TransferStatusPending || code == "" {
		return ErrInvalidTransition
	}
	t.Status = TransferStatusFailed
	t.FailureCode = code
	t.UpdatedAt = now
	return nil
}
