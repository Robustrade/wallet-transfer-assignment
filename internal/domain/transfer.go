package domain

import (
	"fmt"
	"strings"
	"time"
)

// TransferStatus is the state of a transfer.
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// FailureReason explains why a transfer FAILED.
type FailureReason string

const ReasonInsufficientFunds FailureReason = "INSUFFICIENT_FUNDS"

// MaxAmount keeps sums of amounts far away from int64 overflow.
const MaxAmount int64 = 1_000_000_000_000_000

// allowedTransitions: PENDING is the only non-final state.
// PROCESSED and FAILED never change again.
var allowedTransitions = map[TransferStatus][]TransferStatus{
	StatusPending:   {StatusProcessed, StatusFailed},
	StatusProcessed: {},
	StatusFailed:    {},
}

// CanTransition reports whether a transfer may move from one status to another.
func CanTransition(from, to TransferStatus) bool {
	for _, next := range allowedTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// AssertTransition returns ErrInvalidStateTransition if the move is not allowed.
func AssertTransition(from, to TransferStatus) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidStateTransition, from, to)
	}
	return nil
}

// IsFinal reports whether no further transition is possible.
func (s TransferStatus) IsFinal() bool {
	return len(allowedTransitions[s]) == 0
}

// Transfer is a request to move Amount from one wallet to another.
type Transfer struct {
	ID            string         `json:"id"`
	FromWalletID  string         `json:"fromWalletId"`
	ToWalletID    string         `json:"toWalletId"`
	Amount        int64          `json:"amount"`
	Status        TransferStatus `json:"status"`
	FailureReason *FailureReason `json:"failureReason"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// TransferCommand is the input for creating a transfer.
type TransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	// Amount is a positive integer in minor units (e.g. paise/cents).
	Amount int64
}

// Validate checks the business rules of a transfer request.
func (c TransferCommand) Validate() error {
	switch {
	case strings.TrimSpace(c.IdempotencyKey) == "":
		return fmt.Errorf("%w: idempotencyKey must not be empty", ErrValidation)
	case strings.TrimSpace(c.FromWalletID) == "" || strings.TrimSpace(c.ToWalletID) == "":
		return fmt.Errorf("%w: fromWalletId and toWalletId are required", ErrValidation)
	case c.FromWalletID == c.ToWalletID:
		return fmt.Errorf("%w: fromWalletId and toWalletId must be different", ErrValidation)
	case c.Amount <= 0 || c.Amount > MaxAmount:
		return fmt.Errorf("%w: amount must be a positive integer in minor units, at most %d",
			ErrValidation, MaxAmount)
	}
	return nil
}
