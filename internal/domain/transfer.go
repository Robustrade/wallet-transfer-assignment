package domain

import (
	"fmt"
	"time"
)

// TransferState is the lifecycle state of a transfer.
type TransferState string

const (
	// TransferPending is the initial state: the transfer is recorded but the
	// ledger and balances have not yet been mutated.
	TransferPending TransferState = "PENDING"
	// TransferProcessed is a successfully applied transfer.
	TransferProcessed TransferState = "PROCESSED"
	// TransferFailed is a transfer that was rejected by a business rule
	// (e.g. insufficient funds). It is terminal and replayable.
	TransferFailed TransferState = "FAILED"
)

// Valid reports whether s is a known state.
func (s TransferState) Valid() bool {
	switch s {
	case TransferPending, TransferProcessed, TransferFailed:
		return true
	default:
		return false
	}
}

// validTransitions is the whitelist for the transfer state machine.
var validTransitions = map[TransferState]map[TransferState]bool{
	TransferPending:   {TransferProcessed: true, TransferFailed: true},
	TransferProcessed: {},
	TransferFailed:    {},
}

// Transfer is the aggregate for a single wallet-to-wallet movement of funds.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         Money
	State          TransferState
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTransfer builds a PENDING transfer after validating its invariants.
func NewTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount Money) (*Transfer, error) {
	switch {
	case id == "":
		return nil, fmt.Errorf("%w: transfer id is required", ErrValidation)
	case idempotencyKey == "":
		return nil, fmt.Errorf("%w: idempotencyKey is required", ErrValidation)
	case fromWalletID == "":
		return nil, fmt.Errorf("%w: fromWalletId is required", ErrValidation)
	case toWalletID == "":
		return nil, fmt.Errorf("%w: toWalletId is required", ErrValidation)
	case fromWalletID == toWalletID:
		return nil, fmt.Errorf("%w: fromWalletId and toWalletId must differ", ErrValidation)
	case !amount.IsPositive():
		return nil, fmt.Errorf("%w: amount must be positive", ErrValidation)
	}
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		State:          TransferPending,
	}, nil
}

// MarkProcessed moves the transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error { return t.transition(TransferProcessed) }

// MarkFailed moves the transfer to FAILED and records the reason.
func (t *Transfer) MarkFailed(reason string) error {
	if err := t.transition(TransferFailed); err != nil {
		return err
	}
	t.FailureReason = reason
	return nil
}

// IsTerminal reports whether the transfer can no longer change state.
func (t *Transfer) IsTerminal() bool {
	return t.State == TransferProcessed || t.State == TransferFailed
}

func (t *Transfer) transition(to TransferState) error {
	if !t.State.Valid() {
		return fmt.Errorf("%w: current state %q is unknown", ErrInvalidStateTransition, t.State)
	}
	if !validTransitions[t.State][to] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidStateTransition, t.State, to)
	}
	t.State = to
	return nil
}
