package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// TransferStatus is the transfer state-machine state. Transitions are terminal:
// only PENDING -> PROCESSED or PENDING -> FAILED are allowed.
type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

// Valid reports whether s is a known status value.
func (s TransferStatus) Valid() bool {
	switch s {
	case TransferPending, TransferProcessed, TransferFailed:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s is a final state that can never transition again.
func (s TransferStatus) IsTerminal() bool {
	return s == TransferProcessed || s == TransferFailed
}

// CanTransitionTo enforces the state machine. The only legal moves are from
// PENDING to PROCESSED or FAILED; every other transition is rejected.
func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	if s != TransferPending {
		return false
	}
	return next == TransferProcessed || next == TransferFailed
}

// Transfer is a single wallet-to-wallet transfer. It is created PENDING in its
// own committed transaction (claiming idempotency_key) before business logic
// runs, then transitions to a terminal state in a second transaction.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         decimal.Decimal
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ValidateTransferInput checks the invariants that hold independently of any
// persisted state: required identifiers, a strictly positive amount, and
// distinct source and destination wallets.
func ValidateTransferInput(idempotencyKey, fromWalletID, toWalletID string, amount decimal.Decimal) error {
	if idempotencyKey == "" || fromWalletID == "" || toWalletID == "" {
		return ErrMissingField
	}
	if fromWalletID == toWalletID {
		return ErrSameWallet
	}
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
