package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

var allowedTransitions = map[TransferStatus]map[TransferStatus]bool{
	StatusPending: {
		StatusProcessed: true,
		StatusFailed:    true,
	},
	StatusProcessed: {},
	StatusFailed:    {},
}

func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	return allowedTransitions[s][next]
}

type Transfer struct {
	ID            uuid.UUID
	FromWalletID  string
	ToWalletID    string
	Amount        Amount
	Currency      string
	Status        TransferStatus
	FailureReason *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         Amount
}

func (r TransferRequest) Normalize() TransferRequest {
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	r.FromWalletID = NormalizeWalletID(r.FromWalletID)
	r.ToWalletID = NormalizeWalletID(r.ToWalletID)
	return r
}

func (r TransferRequest) Validate() error {
	if err := ValidateIdempotencyKey(r.IdempotencyKey); err != nil {
		return err
	}
	if err := ValidateWalletID("fromWalletId", r.FromWalletID); err != nil {
		return err
	}
	if err := ValidateWalletID("toWalletId", r.ToWalletID); err != nil {
		return err
	}
	if r.FromWalletID == r.ToWalletID {
		return newValidationError("toWalletId", "must differ from fromWalletId")
	}
	return ValidateAmount(r.Amount)
}

func NewPendingTransfer(id uuid.UUID, req TransferRequest, currency string) (*Transfer, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateCurrency(currency); err != nil {
		return nil, err
	}
	return &Transfer{
		ID:           id,
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Currency:     currency,
		Status:       StatusPending,
	}, nil
}

func (t *Transfer) MarkProcessed() error {
	if !t.Status.CanTransitionTo(StatusProcessed) {
		return ErrInvalidStateTransition
	}
	t.Status = StatusProcessed
	t.FailureReason = nil
	return nil
}

func (t *Transfer) MarkFailed(reason string) error {
	if !t.Status.CanTransitionTo(StatusFailed) {
		return ErrInvalidStateTransition
	}
	if strings.TrimSpace(reason) == "" {
		return newValidationError("failureReason", "must not be empty for a failed transfer")
	}
	t.Status = StatusFailed
	t.FailureReason = &reason
	return nil
}
