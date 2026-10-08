package domain

import (
	"fmt"
	"time"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

func (s TransferStatus) Validate() error {
	switch s {
	case TransferStatusPending, TransferStatusProcessed, TransferStatusFailed:
		return nil
	default:
		return fmt.Errorf("invalid transfer status: %q", s)
	}
}

// Transfer starts PENDING and can reach exactly one terminal state.
// Amount is expressed in minor units. The zero value is not a valid transfer.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	status         TransferStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount int64, at time.Time) (Transfer, error) {
	transfer := Transfer{
		ID: id, IdempotencyKey: idempotencyKey, FromWalletID: fromWalletID,
		ToWalletID: toWalletID, Amount: amount, status: TransferStatusPending,
		CreatedAt: at, UpdatedAt: at,
	}
	if err := transfer.Validate(); err != nil {
		return Transfer{}, err
	}
	return transfer, nil
}

func (t Transfer) Validate() error {
	if t.Amount <= 0 {
		return fmt.Errorf("transfer amount must be positive")
	}
	if t.FromWalletID == t.ToWalletID {
		return fmt.Errorf("source and destination wallets must differ")
	}
	return t.status.Validate()
}

func (t Transfer) Status() TransferStatus { return t.status }

func (t *Transfer) MarkProcessed(at time.Time) error {
	return t.transition(TransferStatusProcessed, at)
}

func (t *Transfer) MarkFailed(at time.Time) error {
	return t.transition(TransferStatusFailed, at)
}

func (t *Transfer) transition(target TransferStatus, at time.Time) error {
	if t.status != TransferStatusPending {
		return fmt.Errorf("cannot transition transfer from %q to %q", t.status, target)
	}
	t.status = target
	t.UpdatedAt = at
	return nil
}
