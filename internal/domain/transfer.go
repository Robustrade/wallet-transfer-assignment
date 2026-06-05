package domain

import (
	"errors"

	"github.com/google/uuid"
)

type TransferState string

const (
	Pending   TransferState = "PENDING"
	Processed TransferState = "PROCESSED"
	Failed    TransferState = "FAILED"
)

type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string

	FromWalletID uuid.UUID
	ToWalletID   uuid.UUID

	Amount int64

	State TransferState
}

func (t *Transfer) MarkProcessed() error {

	if t.State != Pending {
		return errors.New("invalid transition")
	}

	t.State = Processed
	return nil
}

func (t *Transfer) MarkFailed() error {

	if t.State != Pending {
		return errors.New("invalid transition")
	}

	t.State = Failed
	return nil
}
