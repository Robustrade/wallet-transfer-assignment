package domain

import (
	"errors"
	"fmt"
)

// TransferState is where a transfer is in its lifecycle. PROCESSED and FAILED
// are final.
type TransferState string

const (
	StatePending   TransferState = "PENDING"
	StateProcessed TransferState = "PROCESSED"
	StateFailed    TransferState = "FAILED"
)

// ErrInvalidTransition means a transfer was asked to leave a final state.
var ErrInvalidTransition = errors.New("invalid transfer state transition")

// Transfer moves Amount from one wallet to another.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	State          TransferState
}

// NewTransfer creates a PENDING transfer for a validated request.
func NewTransfer(id string, req TransferRequest) Transfer {
	return Transfer{
		ID:             id,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		State:          StatePending,
	}
}

// Matches reports whether req has the same body as the request that created
// t: the same wallets and amount (spec §5).
func (t Transfer) Matches(req TransferRequest) bool {
	return t.FromWalletID == req.FromWalletID &&
		t.ToWalletID == req.ToWalletID &&
		t.Amount == req.Amount
}

// MarkProcessed moves a PENDING transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	return t.moveTo(StateProcessed)
}

// MarkFailed moves a PENDING transfer to FAILED.
func (t *Transfer) MarkFailed() error {
	return t.moveTo(StateFailed)
}

// moveTo allows only PENDING -> PROCESSED and PENDING -> FAILED.
func (t *Transfer) moveTo(next TransferState) error {
	if t.State != StatePending {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, t.State, next)
	}
	t.State = next
	return nil
}
