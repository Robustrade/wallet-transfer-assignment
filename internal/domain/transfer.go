package domain

import (
	"fmt"
	"time"
)

// TransferStatus represents the state of a transfer.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// Transfer represents a wallet-to-wallet money transfer.
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	FailureReason  string         `json:"failureReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// NewTransfer creates a new transfer in PENDING state.
func NewTransfer(idempotencyKey, fromWalletID, toWalletID string, amount int64) (*Transfer, error) {
	if idempotencyKey == "" {
		return nil, ErrEmptyIdempotencyKey
	}
	if fromWalletID == "" || toWalletID == "" {
		return nil, ErrEmptyWalletID
	}
	if fromWalletID == toWalletID {
		return nil, ErrSameWallet
	}
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	return &Transfer{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         TransferStatusPending,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}, nil
}

// MarkProcessed transitions the transfer to PROCESSED state.
// Only valid from PENDING state.
func (t *Transfer) MarkProcessed() error {
	if t.Status != TransferStatusPending {
		return fmt.Errorf("%w: cannot transition from %s to PROCESSED", ErrInvalidStateTransition, t.Status)
	}
	t.Status = TransferStatusProcessed
	t.UpdatedAt = time.Now()
	return nil
}

// MarkFailed transitions the transfer to FAILED state with a reason.
// Only valid from PENDING state.
func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != TransferStatusPending {
		return fmt.Errorf("%w: cannot transition from %s to FAILED", ErrInvalidStateTransition, t.Status)
	}
	t.Status = TransferStatusFailed
	t.FailureReason = reason
	t.UpdatedAt = time.Now()
	return nil
}

// IsTerminal returns true if the transfer is in a terminal state (PROCESSED or FAILED).
func (t *Transfer) IsTerminal() bool {
	return t.Status == TransferStatusProcessed || t.Status == TransferStatusFailed
}
