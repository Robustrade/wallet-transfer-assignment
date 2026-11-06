package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Field limits; they mirror the CHECK constraints in the schema.
const (
	MaxIdempotencyKeyLength = 255
	MaxWalletIDLength       = 64
)

// TransferStatus is a state in the transfer lifecycle:
//
//	PENDING ──▶ PROCESSED
//	   └──────▶ FAILED
//
// PROCESSED and FAILED are terminal.
type TransferStatus string

// Transfer states.
const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// FailureReason explains why a transfer ended in StatusFailed.
type FailureReason string

// Failure reasons.
const (
	FailureInsufficientFunds FailureReason = "INSUFFICIENT_FUNDS"
)

// TransferRequest is a client's intent to move Amount from one wallet to another.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Validate checks the request against the business rules and returns a *ValidationError.
func (r TransferRequest) Validate() error {
	if err := requireText("idempotencyKey", r.IdempotencyKey, MaxIdempotencyKeyLength); err != nil {
		return err
	}
	if err := requireText("fromWalletId", r.FromWalletID, MaxWalletIDLength); err != nil {
		return err
	}
	if err := requireText("toWalletId", r.ToWalletID, MaxWalletIDLength); err != nil {
		return err
	}
	if r.FromWalletID == r.ToWalletID {
		return invalid("toWalletId", "must differ from fromWalletId")
	}
	if r.Amount <= 0 {
		return invalid("amount", "must be a positive integer")
	}
	return nil
}

func requireText(field, value string, maxLen int) error {
	if strings.TrimSpace(value) == "" {
		return invalid(field, "is required")
	}
	if utf8.RuneCountInString(value) > maxLen {
		return invalid(field, fmt.Sprintf("must be at most %d characters", maxLen))
	}
	return nil
}

// Transfer moves money between two wallets. Its state only changes through
// Execute, Process and Fail, which enforce the lifecycle.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  FailureReason // empty unless Status is StatusFailed
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewPendingTransfer validates req and creates a transfer in StatusPending.
func NewPendingTransfer(id uuid.UUID, req TransferRequest, now time.Time) (Transfer, error) {
	if err := req.Validate(); err != nil {
		return Transfer{}, err
	}
	return Transfer{
		ID:             id,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Execute settles a pending transfer against its (locked) source wallet: it is
// processed when the wallet can cover the amount and failed otherwise.
// Insufficient funds is a business outcome, not an error.
func (t *Transfer) Execute(source Wallet, now time.Time) error {
	if t.Status != StatusPending {
		return ErrInvalidTransition
	}
	if source.ID != t.FromWalletID {
		return fmt.Errorf("execute transfer %s: wallet %q is not the source wallet %q", t.ID, source.ID, t.FromWalletID)
	}
	if !source.CanDebit(t.Amount) {
		return t.Fail(FailureInsufficientFunds, now)
	}
	return t.Process(now)
}

// Process moves a pending transfer to StatusProcessed.
func (t *Transfer) Process(now time.Time) error {
	if t.Status != StatusPending {
		return ErrInvalidTransition
	}
	t.Status = StatusProcessed
	t.UpdatedAt = now
	return nil
}

// Fail moves a pending transfer to StatusFailed with the given reason.
func (t *Transfer) Fail(reason FailureReason, now time.Time) error {
	if t.Status != StatusPending {
		return ErrInvalidTransition
	}
	if reason == "" {
		return fmt.Errorf("fail transfer %s: a failure reason is required", t.ID)
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	t.UpdatedAt = now
	return nil
}
