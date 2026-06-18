package domain

import "errors"

type TransferStatus string

const (
	Pending   TransferStatus = "PENDING"
	Processed TransferStatus = "PROCESSED"
	Failed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID             string
	IdempotencyKey string

	FromWalletID string
	ToWalletID   string

	Amount int64

	Status TransferStatus
}

var (
	ErrInvalidStateTransition = errors.New("invalid state transition")
)

func (t *Transfer) MarkProcessed() error {
	if t.Status != Pending {
		return ErrInvalidStateTransition
	}

	t.Status = Processed
	return nil
}

func (t *Transfer) MarkFailed() error {
	if t.Status != Pending {
		return ErrInvalidStateTransition
	}

	t.Status = Failed
	return nil
}
