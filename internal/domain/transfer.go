package domain

import "time"

type TransferState string

const (
	TransferPending   TransferState = "PENDING"
	TransferProcessed TransferState = "PROCESSED"
	TransferFailed    TransferState = "FAILED"
)

var transitions = map[TransferState]map[TransferState]bool{
	TransferPending: {
		TransferProcessed: true,
		TransferFailed:    true,
	},
}

func CanTransition(from, to TransferState) bool {
	next, ok := transitions[from]
	if !ok {
		return false
	}
	return next[to]
}

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

// Basic validations to check if transfer is valid.
func NewTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*Transfer, error) {
	if id == "" || idempotencyKey == "" {
		return nil, ErrInvalidRequest
	}
	if fromWalletID == "" || toWalletID == "" {
		return nil, ErrInvalidRequest
	}
	if fromWalletID == toWalletID {
		return nil, ErrSameWallet
	}
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	now := time.Now().UTC()
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		State:          TransferPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Mark Processed transitions as PENDING transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	if !CanTransition(t.State, TransferProcessed) {
		return ErrInvalidStateTransition
	}
	t.State = TransferProcessed
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// Mark Failed transitions as PENDING transfer to FAILED with a reason.
func (t *Transfer) MarkFailed(reason string) error {
	if !CanTransition(t.State, TransferFailed) {
		return ErrInvalidStateTransition
	}
	t.State = TransferFailed
	t.FailureReason = reason
	t.UpdatedAt = time.Now().UTC()
	return nil
}
