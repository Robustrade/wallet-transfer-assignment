package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidTransition is returned when a transfer's status is moved
// outside the PENDING -> {PROCESSED, FAILED} rule.
var ErrInvalidTransition = errors.New("invalid transfer state transition")

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID            uuid.UUID
	FromWalletID  string
	ToWalletID    string
	Amount        int64
	Status        TransferStatus
	FailureReason *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewTransfer builds a transfer in its initial PENDING state.
func NewTransfer(fromWalletID, toWalletID string, amount int64) *Transfer {
	now := time.Now().UTC()
	return &Transfer{
		ID:           uuid.New(),
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
		Status:       TransferPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// CanTransition reports whether moving from the transfer's current status
// to the given status is a legal state transition. Only PENDING transfers
// may resolve, and only into a terminal state.
func (t *Transfer) CanTransition(to TransferStatus) bool {
	if t.Status != TransferPending {
		return false
	}
	return to == TransferProcessed || to == TransferFailed
}

// MarkProcessed transitions the transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	if !t.CanTransition(TransferProcessed) {
		return ErrInvalidTransition
	}
	t.Status = TransferProcessed
	return nil
}

// MarkFailed transitions the transfer to FAILED with a reason.
func (t *Transfer) MarkFailed(reason string) error {
	if !t.CanTransition(TransferFailed) {
		return ErrInvalidTransition
	}
	t.Status = TransferFailed
	t.FailureReason = &reason
	return nil
}
