package domain

import (
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TransferState is the lifecycle of a transfer.
type TransferState string

const (
	StatePending    TransferState = "PENDING"
	StateProcessing TransferState = "PROCESSING"
	StateProcessed  TransferState = "PROCESSED"
	StateFailed     TransferState = "FAILED"
)

func (s TransferState) Valid() bool {
	switch s {
	case StatePending, StateProcessing, StateProcessed, StateFailed:
		return true
	}
	return false
}

// IsTerminal reports whether no further transition is allowed.
func (s TransferState) IsTerminal() bool {
	return s == StateProcessed || s == StateFailed
}

// allowedTransitions encodes the state machine:
//
//	PENDING    -> PROCESSING              (worker claims)
//	PROCESSING -> PROCESSED | FAILED      (worker finishes)
//	PROCESSING -> PENDING                 (reaper requeues an abandoned claim)
var allowedTransitions = map[TransferState]map[TransferState]bool{
	StatePending:    {StateProcessing: true},
	StateProcessing: {StateProcessed: true, StateFailed: true, StatePending: true},
}

// CanTransitionTo guards state changes so retries/duplicates cannot move a
// transfer illegally (e.g. re-processing a terminal transfer).
func (s TransferState) CanTransitionTo(next TransferState) bool {
	return allowedTransitions[s][next]
}

// Transfer is both the intent (what was requested) and the idempotency record.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         decimal.Decimal
	State          TransferState
	FailureReason  *string
	Attempts       int
	ClaimedAt      *time.Time
	WorkerID       *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MaxIdempotencyKeyLength matches the idempotency_key column (VARCHAR(64)). Keys
// are validated against this so an over-long key is rejected as a 400 instead of
// reaching the database and becoming a 500.
const MaxIdempotencyKeyLength = 64

// NewTransferParams is the validated input needed to enqueue a transfer.
type NewTransferParams struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         decimal.Decimal
}

// Validate enforces request-level rules that never depend on stored state.
func (p NewTransferParams) Validate() error {
	if p.IdempotencyKey == "" {
		return ErrEmptyIdempotencyKey
	}
	if utf8.RuneCountInString(p.IdempotencyKey) > MaxIdempotencyKeyLength {
		return ErrIdempotencyKeyTooLong
	}
	if !p.Amount.IsPositive() {
		return ErrInvalidAmount
	}
	if p.FromWalletID == p.ToWalletID {
		return ErrSameWallet
	}
	return nil
}
