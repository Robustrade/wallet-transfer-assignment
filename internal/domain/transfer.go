package domain

import "time"

// TransferStatus models the transfer state machine described in solution.md:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// Once a transfer reaches PROCESSED or FAILED it is terminal; duplicate
// requests are served from that terminal state rather than re-entering the
// machine.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// Transfer is the aggregate root for a wallet-to-wallet money movement,
// uniquely identified (for API-level exactly-once semantics) by its
// IdempotencyKey.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CanTransitionTo reports whether moving from the transfer's current status
// to `next` is a legal state transition.
func (t Transfer) CanTransitionTo(next TransferStatus) bool {
	switch t.Status {
	case TransferStatusPending:
		return next == TransferStatusProcessed || next == TransferStatusFailed
	default:
		// PROCESSED and FAILED are terminal states.
		return false
	}
}

// Validate checks the structural invariants of a transfer request before
// any persistence or business logic runs.
func (t Transfer) Validate() error {
	if t.IdempotencyKey == "" {
		return ErrMissingIdempotencyKey
	}
	if t.FromWalletID == t.ToWalletID {
		return ErrSameWallet
	}
	if t.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
