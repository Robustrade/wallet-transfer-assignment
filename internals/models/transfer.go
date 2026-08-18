package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type TransferState string

const (
	TransferPending   TransferState = "PENDING"
	TransferProcessed TransferState = "PROCESSED"
	TransferFailed    TransferState = "FAILED"
)

var (
	ErrInvalidTransition = errors.New("invalid transfer state transition")
	ErrSameWallet        = errors.New("from and to wallet must be different")
	ErrNonPositiveAmount = errors.New("amount must be positive")
)

// Transfer is the domain entity for a wallet-to-wallet transfer request.
// State transitions are only valid PENDING -> PROCESSED and PENDING -> FAILED.
// This is enforced here so no calling code can accidentally re-process or
// flip a terminal transfer.
type Transfer struct {
	ID             uuid.UUID
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
	State          TransferState
	FailureReason  string
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTransfer validates and constructs a new PENDING transfer.
func NewTransfer(fromWalletID, toWalletID uuid.UUID, amount int64, idempotencyKey string) (*Transfer, error) {
	if fromWalletID == toWalletID {
		return nil, ErrSameWallet
	}
	if amount <= 0 {
		return nil, ErrNonPositiveAmount
	}
	if idempotencyKey == "" {
		return nil, errors.New("idempotencyKey is required")
	}

	return &Transfer{
		ID:             uuid.New(),
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		State:          TransferPending,
		IdempotencyKey: idempotencyKey,
	}, nil
}

// MarkProcessed transitions PENDING -> PROCESSED. Rejects any other starting state.
func (t *Transfer) MarkProcessed() error {
	if t.State != TransferPending {
		return ErrInvalidTransition
	}
	t.State = TransferProcessed
	return nil
}

// MarkFailed transitions PENDING -> FAILED with a reason. Rejects any other starting state.
func (t *Transfer) MarkFailed(reason string) error {
	if t.State != TransferPending {
		return ErrInvalidTransition
	}
	t.State = TransferFailed
	t.FailureReason = reason
	return nil
}

// IsTerminal reports whether the transfer has reached a final state.
func (t *Transfer) IsTerminal() bool {
	return t.State == TransferProcessed || t.State == TransferFailed
}
