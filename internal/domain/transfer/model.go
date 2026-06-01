package transfer

import (
	"errors"
	"time"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/timeutil"
)

// Status represents the state machine for a transfer.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusProcessed Status = "PROCESSED"
	StatusFailed    Status = "FAILED"
)

// Transfer is the core domain entity for a wallet-to-wallet movement of funds.
//
// Design notes:
//   - Pure domain model — no storage tags, no DB imports.
//   - ID is domain-generated (prefixed UUID v7). Format: "txn_<uuidv7hex>"
//   - TxnID is the idempotency key supplied by the caller; stored with a UNIQUE
//     constraint in the DB. A duplicate request is detected by looking up TxnID
//     before any write — no separate idempotency table needed.
//   - State machine: PENDING → PROCESSED or PENDING → FAILED only.
type Transfer struct {
	ID           string
	TxnID        string // caller-supplied idempotency key (unique per DB constraint)
	FromWalletID string
	ToWalletID   string
	Amount       int64
	Status       Status
	FailureReason string    // populated only when Status == FAILED
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Validate checks the internal consistency of a Transfer.
func (t *Transfer) Validate() error {
	if t.ID == "" {
		return errors.New("transfer: id is required")
	}
	if t.TxnID == "" {
		return errors.New("transfer: txnId (idempotency key) is required")
	}
	if t.FromWalletID == "" {
		return errors.New("transfer: fromWalletId is required")
	}
	if t.ToWalletID == "" {
		return errors.New("transfer: toWalletId is required")
	}
	if t.FromWalletID == t.ToWalletID {
		return errors.New("transfer: fromWalletId and toWalletId must differ")
	}
	if t.Amount <= 0 {
		return errors.New("transfer: amount must be positive")
	}
	return nil
}

// CanTransitionTo returns true if the given status is a valid next state.
// Allowed transitions: PENDING → PROCESSED, PENDING → FAILED.
func (t *Transfer) CanTransitionTo(next Status) bool {
	if t.Status == StatusPending && (next == StatusProcessed || next == StatusFailed) {
		return true
	}
	return false
}

// MarkProcessed moves the transfer to PROCESSED state.
func (t *Transfer) MarkProcessed() error {
	if !t.CanTransitionTo(StatusProcessed) {
		return errors.New("transfer: invalid state transition to PROCESSED")
	}
	t.Status = StatusProcessed
	t.UpdatedAt = timeutil.Now()
	return nil
}

// MarkFailed moves the transfer to FAILED state with a reason.
func (t *Transfer) MarkFailed(reason string) error {
	if !t.CanTransitionTo(StatusFailed) {
		return errors.New("transfer: invalid state transition to FAILED")
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	t.UpdatedAt = timeutil.Now()
	return nil
}
