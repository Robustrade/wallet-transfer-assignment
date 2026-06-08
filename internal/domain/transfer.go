package domain

import (
	"fmt"
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
	ID            uuid.UUID
	FromWalletID  uuid.UUID
	ToWalletID    uuid.UUID
	Amount        Amount
	Status        TransferStatus
	FailureReason string
	CreatedAt     time.Time
	ProcessedAt   *time.Time
}

// Transition moves the transfer to a new status, enforcing the state machine:
//
//	PENDING → PROCESSED
//	PENDING → FAILED
func (t *Transfer) Transition(to TransferStatus) error {
	if t.Status != StatusPending {
		return fmt.Errorf("transfer %s: invalid transition %s → %s", t.ID, t.Status, to)
	}
	if to != StatusProcessed && to != StatusFailed {
		return fmt.Errorf("transfer %s: unknown target status %s", t.ID, to)
	}
	t.Status = to
	return nil
}
