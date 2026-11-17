package model

import (
	"strings"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/google/uuid"
)

// Transfer status values. Only PENDING → PROCESSED | FAILED are valid transitions.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusProcessed Status = "PROCESSED"
	StatusFailed    Status = "FAILED"
)

// FailureInsufficientFunds is the only failure reason in this version.
const FailureInsufficientFunds = "insufficient_funds"

// Transfer is one wallet-to-wallet movement claimed by an idempotency key.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         helpers.Money
	Status         Status
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTransfer builds a PENDING transfer between two distinct wallets.
func NewTransfer(idempotencyKey, fromWalletID, toWalletID string, amount int64) (*Transfer, error) {
	key, err := NormalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	from, err := NormalizeID(fromWalletID)
	if err != nil {
		return nil, err
	}
	to, err := NormalizeID(toWalletID)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, ErrSameWallet
	}
	amt, err := NewAmount(amount)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Transfer{
		ID:             uuid.New(),
		IdempotencyKey: key,
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         amt,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// MarkProcessed transitions PENDING → PROCESSED. Terminal states reject the call.
func (t *Transfer) MarkProcessed() error {
	if t.Status != StatusPending {
		return ErrInvalidTransition
	}
	t.Status = StatusProcessed
	t.FailureReason = nil
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkFailed transitions PENDING → FAILED with a non-empty reason.
func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != StatusPending {
		return ErrInvalidTransition
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrInvalidFailureReason
	}
	t.Status = StatusFailed
	t.FailureReason = &reason
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// IsTerminal reports whether the transfer is PROCESSED or FAILED.
func (t *Transfer) IsTerminal() bool {
	return t.Status == StatusProcessed || t.Status == StatusFailed
}
