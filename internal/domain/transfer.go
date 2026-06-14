package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// TransferStatus represents the lifecycle state of a transfer.
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// Transfer is the aggregate root for a wallet-to-wallet movement of funds.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validation errors returned before any DB interaction.
var (
	ErrAmountNotPositive  = errors.New("amount must be greater than zero")
	ErrSameWallet         = errors.New("from and to wallet must differ")
	ErrMissingWalletID    = errors.New("wallet IDs must not be empty")
	ErrMissingIdempotency = errors.New("idempotency key must not be empty")
)

// Validate checks business-level invariants that are independent of
// persistence state. Call this before passing the transfer to the service.
func (t *Transfer) Validate() error {
	if t.IdempotencyKey == "" {
		return ErrMissingIdempotency
	}
	if t.FromWalletID == "" || t.ToWalletID == "" {
		return ErrMissingWalletID
	}
	if t.FromWalletID == t.ToWalletID {
		return ErrSameWallet
	}
	if t.Amount <= 0 {
		return ErrAmountNotPositive
	}
	return nil
}

// MarkProcessed transitions the transfer to PROCESSED. Returns an error if
// the current state does not allow this transition.
func (t *Transfer) MarkProcessed() error {
	if t.Status != StatusPending {
		return ErrInvalidTransition{From: t.Status, To: StatusProcessed}
	}
	t.Status = StatusProcessed
	return nil
}

// MarkFailed transitions the transfer to FAILED with a human-readable reason.
func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != StatusPending {
		return ErrInvalidTransition{From: t.Status, To: StatusFailed}
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	return nil
}

// ErrInvalidTransition describes an illegal state machine move.
type ErrInvalidTransition struct {
	From TransferStatus
	To   TransferStatus
}

func (e ErrInvalidTransition) Error() string {
	return "invalid transition from " + string(e.From) + " to " + string(e.To)
}
