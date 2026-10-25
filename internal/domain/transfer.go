package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const maxIdempotencyKeyLen = 255

// TransferStatus is the lifecycle state of a transfer.
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// PROCESSED and FAILED are terminal.
type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

// FailureReason explains why a transfer ended in FAILED.
type FailureReason string

const (
	FailureInsufficientFunds FailureReason = "INSUFFICIENT_FUNDS"
	FailureBalanceOverflow   FailureReason = "BALANCE_OVERFLOW"
)

// Transfer moves Amount minor units from one wallet to another.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  FailureReason
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTransfer validates the request and returns a PENDING transfer.
func NewTransfer(id uuid.UUID, idempotencyKey, fromWalletID, toWalletID string, amount int64, now time.Time) (*Transfer, error) {
	if err := ValidateTransferRequest(idempotencyKey, fromWalletID, toWalletID, amount); err != nil {
		return nil, err
	}
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         TransferPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// ValidateTransferRequest checks the invariants every transfer request must meet.
func ValidateTransferRequest(idempotencyKey, fromWalletID, toWalletID string, amount int64) error {
	switch {
	case !validText(idempotencyKey, maxIdempotencyKeyLen):
		return fmt.Errorf("%w: idempotencyKey must be 1-%d characters of valid text", ErrInvalidRequest, maxIdempotencyKeyLen)
	case !validText(fromWalletID, maxWalletIDLen) || !validText(toWalletID, maxWalletIDLen):
		return fmt.Errorf("%w: fromWalletId and toWalletId are required (1-%d characters of valid text)", ErrInvalidRequest, maxWalletIDLen)
	case fromWalletID == toWalletID:
		return fmt.Errorf("%w: cannot transfer to the same wallet", ErrInvalidRequest)
	case amount <= 0:
		return fmt.Errorf("%w: amount must be positive", ErrInvalidRequest)
	}
	return nil
}

// MarkProcessed moves a PENDING transfer to PROCESSED.
func (t *Transfer) MarkProcessed(now time.Time) error {
	if t.Status != TransferPending {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, TransferProcessed)
	}
	t.Status = TransferProcessed
	t.UpdatedAt = now
	return nil
}

// MarkFailed moves a PENDING transfer to FAILED, recording why.
func (t *Transfer) MarkFailed(reason FailureReason, now time.Time) error {
	if t.Status != TransferPending {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, TransferFailed)
	}
	t.Status = TransferFailed
	t.FailureReason = reason
	t.UpdatedAt = now
	return nil
}
