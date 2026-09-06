package domain

import "time"

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  string
	RequestHash    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTransfer validates the inputs for a transfer request and returns a new
// PENDING transfer. It does not touch balances or persistence.
func NewTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount int64, requestHash string, now time.Time) (*Transfer, error) {
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if fromWalletID == toWalletID {
		return nil, ErrSameWallet
	}
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         TransferPending,
		RequestHash:    requestHash,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// canTransitionTo enforces the allowed state machine edges:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// PROCESSED and FAILED are terminal.
func (t *Transfer) canTransitionTo(next TransferStatus) bool {
	if t.Status != TransferPending {
		return false
	}
	return next == TransferProcessed || next == TransferFailed
}

func (t *Transfer) MarkProcessed(now time.Time) error {
	if !t.canTransitionTo(TransferProcessed) {
		return ErrInvalidStateTransition
	}
	t.Status = TransferProcessed
	t.UpdatedAt = now
	return nil
}

func (t *Transfer) MarkFailed(reason string, now time.Time) error {
	if !t.canTransitionTo(TransferFailed) {
		return ErrInvalidStateTransition
	}
	t.Status = TransferFailed
	t.FailureReason = reason
	t.UpdatedAt = now
	return nil
}
