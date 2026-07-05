package domain

import (
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
	FromWalletID  string
	ToWalletID    string
	Amount        int64
	Status        TransferStatus
	FailureReason *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewPendingTransfer(id uuid.UUID, fromWalletID, toWalletID string, amount int64) (Transfer, error) {
	if fromWalletID == "" || toWalletID == "" {
		return Transfer{}, ErrWalletIdRequired
	}
	if fromWalletID == toWalletID {
		return Transfer{}, ErrSameWallet
	}
	if amount <= 0 {
		return Transfer{}, ErrInvalidAmount
	}
	now := time.Now().Truncate(time.Microsecond)
	return Transfer{
		ID:           id,
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
		Status:       StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (t *Transfer) MarkProcessed() error {
	if t.Status != StatusPending {
		return ErrInvalidTransition
	}
	t.Status = StatusProcessed
	t.UpdatedAt = time.Now().Truncate(time.Microsecond)
	return nil
}

func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != StatusPending {
		return ErrInvalidTransition
	}
	if reason == "" {
		reason = "unspecified"
	}
	t.Status = StatusFailed
	t.FailureReason = &reason
	t.UpdatedAt = time.Now().Truncate(time.Microsecond)
	return nil
}
