package domain

import "time"

// TransferStatus is the lifecycle state of a transfer.
type TransferStatus string

// Allowed transfer states. A transfer starts PENDING and moves exactly once to
// either PROCESSED or FAILED.
const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// Failure reason codes recorded on a FAILED transfer for auditability.
const (
	ReasonInsufficientFunds = "INSUFFICIENT_FUNDS"
	ReasonCurrencyMismatch  = "CURRENCY_MISMATCH"
)

// Transfer represents a wallet-to-wallet value movement. The idempotency key
// makes the operation safe to retry: the same key always maps to the same
// transfer.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Currency       string
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTransfer constructs a PENDING transfer. Identity and timestamps are
// supplied by the caller so the service layer controls id generation and time,
// which keeps the domain deterministic and easy to test. The currency is
// derived from the participating wallets during processing, so it is not part
// of the client request.
func NewTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount int64) *Transfer {
	now := time.Now().UTC()
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Currency:       DefaultCurrency,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// MarkProcessed transitions a PENDING transfer to PROCESSED. Transitioning from
// any other state is rejected, which keeps the state machine safe under retries
// and duplicate delivery.
func (t *Transfer) MarkProcessed() error {
	if t.Status != StatusPending {
		return ErrInvalidStateTransition
	}
	t.Status = StatusProcessed
	t.FailureReason = ""
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkFailed transitions a PENDING transfer to FAILED with a reason code.
func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != StatusPending {
		return ErrInvalidStateTransition
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// IsTerminal reports whether the transfer has reached a final state.
func (t *Transfer) IsTerminal() bool {
	return t.Status == StatusProcessed || t.Status == StatusFailed
}

// SameParams reports whether two transfers describe the same value movement.
// It is used to detect idempotency keys replayed with a different payload. Only
// client-supplied fields are compared; currency is derived from the wallets
// during processing and therefore excluded.
func (t *Transfer) SameParams(other *Transfer) bool {
	return t.FromWalletID == other.FromWalletID &&
		t.ToWalletID == other.ToWalletID &&
		t.Amount == other.Amount
}
