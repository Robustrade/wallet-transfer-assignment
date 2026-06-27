package domain

import "time"

// TransferStatus is the state of a transfer in its lifecycle.
type TransferStatus string

const (
	// StatusPending is the initial state: the transfer has been recorded but
	// not yet applied to wallet balances.
	StatusPending TransferStatus = "PENDING"
	// StatusProcessed is a terminal state: balances and ledger entries have
	// been committed.
	StatusProcessed TransferStatus = "PROCESSED"
	// StatusFailed is a terminal state: the transfer could not be applied (for
	// example, insufficient funds). It is recorded durably so that retries with
	// the same idempotency key return the same failed outcome.
	StatusFailed TransferStatus = "FAILED"
)

// validTransitions encodes the allowed transfer state machine:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// Terminal states have no outgoing transitions, which makes replays and
// duplicate deliveries safe: a committed transfer can never be re-applied.
var validTransitions = map[TransferStatus]map[TransferStatus]bool{
	StatusPending: {
		StatusProcessed: true,
		StatusFailed:    true,
	},
	StatusProcessed: {},
	StatusFailed:    {},
}

// Transfer is the aggregate describing a single wallet-to-wallet movement. The
// idempotency key is part of the aggregate so that exactly-once semantics are
// anchored to the transfer itself.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         Money
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTransfer constructs a PENDING transfer after validating its invariants.
// It does not touch balances; applying the transfer is the service's job.
func NewTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount Money) (*Transfer, error) {
	if idempotencyKey == "" {
		return nil, ErrMissingIdempotencyKey
	}
	if fromWalletID == "" || toWalletID == "" {
		return nil, ErrMissingWalletID
	}
	if fromWalletID == toWalletID {
		return nil, ErrSameWallet
	}
	if err := ValidateAmount(amount); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// CanTransition reports whether moving to the target status is permitted from
// the current status.
func (t *Transfer) CanTransition(to TransferStatus) bool {
	return validTransitions[t.Status][to]
}

// MarkProcessed transitions the transfer to PROCESSED, returning
// ErrInvalidStateTransition if the current state does not allow it.
func (t *Transfer) MarkProcessed() error {
	if !t.CanTransition(StatusProcessed) {
		return ErrInvalidStateTransition
	}
	t.Status = StatusProcessed
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkFailed transitions the transfer to FAILED with a human-readable reason,
// returning ErrInvalidStateTransition if the current state does not allow it.
func (t *Transfer) MarkFailed(reason string) error {
	if !t.CanTransition(StatusFailed) {
		return ErrInvalidStateTransition
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	t.UpdatedAt = time.Now().UTC()
	return nil
}
