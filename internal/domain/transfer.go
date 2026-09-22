package domain

import "time"

// TransferState models the lifecycle of a transfer request.
type TransferState string

const (
	TransferPending   TransferState = "PENDING"
	TransferProcessed TransferState = "PROCESSED"
	TransferFailed    TransferState = "FAILED"
)

// validTransitions enumerates the allowed state machine edges. It is the
// single source of truth for what transitions are legal, keeping the rule
// unit-testable independent of persistence or transport.
var validTransitions = map[TransferState]map[TransferState]bool{
	TransferPending: {
		TransferProcessed: true,
		TransferFailed:    true,
	},
}

// CanTransition reports whether moving a transfer from `from` to `to` is a
// legal state transition.
func CanTransition(from, to TransferState) bool {
	return validTransitions[from][to]
}

// Transfer is a request to move funds from one wallet to another.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	State          TransferState
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Transition moves the transfer to newState, returning ErrInvalidStateTransition
// if the move is not allowed by the state machine.
func (t *Transfer) Transition(newState TransferState, failureReason string) error {
	if !CanTransition(t.State, newState) {
		return ErrInvalidStateTransition
	}
	t.State = newState
	t.FailureReason = failureReason
	return nil
}
