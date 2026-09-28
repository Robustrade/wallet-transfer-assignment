package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle state of a transfer.
//
//	PENDING ──MarkProcessed──▶ PROCESSED
//	   └──────MarkFailed─────▶ FAILED
//
// PROCESSED and FAILED are final.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusProcessed Status = "PROCESSED"
	StatusFailed    Status = "FAILED"
)

// Transfer is a request to move Amount from one wallet to another. It is
// stored whether it succeeds (PROCESSED) or breaks a business rule (FAILED),
// so that an idempotent retry can replay the original outcome.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         Status
	FailureReason  FailureReason // empty unless Status is FAILED
	CreatedAt      time.Time
}

// NewTransfer creates a PENDING transfer with a fresh UUIDv7 id.
//
// UUIDv7 ids start with a timestamp, so they sort roughly by creation time
// and index better than random v4 ids.
//
// now is truncated to microseconds in UTC, because that is what both
// databases store. Otherwise the createdAt in the first response could differ
// from the one returned on a replay, which is read back from the database.
func NewTransfer(idempotencyKey, fromWalletID, toWalletID string, amount int64, now time.Time) (*Transfer, error) {
	// These mirror the database CHECK constraints. Full request validation
	// (lengths, character set, amount limit) lives in the service command.
	switch {
	case idempotencyKey == "":
		return nil, fmt.Errorf("%w: idempotency key is required", ErrValidation)
	case fromWalletID == "" || toWalletID == "":
		return nil, fmt.Errorf("%w: both wallet ids are required", ErrValidation)
	case fromWalletID == toWalletID:
		return nil, fmt.Errorf("%w: cannot transfer to the same wallet", ErrValidation)
	case amount <= 0:
		return nil, fmt.Errorf("%w: amount must be positive, got %d", ErrValidation, amount)
	}

	return &Transfer{
		// NewV7 fails only if the OS random source fails, which nothing can
		// recover from, so Must (panic) is acceptable here.
		ID:             uuid.Must(uuid.NewV7()),
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         StatusPending,
		CreatedAt:      now.UTC().Truncate(time.Microsecond),
	}, nil
}

// MarkProcessed moves a PENDING transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	if err := t.requirePending(StatusProcessed); err != nil {
		return err
	}
	t.Status = StatusProcessed
	return nil
}

// MarkFailed moves a PENDING transfer to FAILED with a known reason.
func (t *Transfer) MarkFailed(reason FailureReason) error {
	if !reason.valid() {
		return fmt.Errorf("%w: unknown failure reason %q", ErrValidation, reason)
	}
	if err := t.requirePending(StatusFailed); err != nil {
		return err
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	return nil
}

// requirePending allows a transition only from PENDING. The repository
// enforces the same rule with a guarded UPDATE (... WHERE status = 'PENDING'),
// so it also holds across processes.
func (t *Transfer) requirePending(target Status) error {
	if t.Status != StatusPending {
		return fmt.Errorf("transfer %s: %s -> %s: %w", t.ID, t.Status, target, ErrInvalidTransition)
	}
	return nil
}
