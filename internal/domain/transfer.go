package domain

import (
	"time"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID            string
	FromWalletID  string
	ToWalletID    string
	Amount        int64
	Status        TransferStatus
	FailureReason string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewTransfer(fromWalletID, toWalletID string, amount int64) (*Transfer, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if fromWalletID == toWalletID {
		return nil, ErrSameWallet
	}

	return &Transfer{
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
		Status:       TransferStatusPending,
	}, nil
}

func (t *Transfer) MarkProcessed() error {
	if t.Status != TransferStatusPending {
		return ErrInvalidTransferTransition
	}
	t.Status = TransferStatusProcessed
	return nil
}

func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != TransferStatusPending {
		return ErrInvalidTransferTransition
	}
	t.Status = TransferStatusFailed
	t.FailureReason = reason
	return nil
}
