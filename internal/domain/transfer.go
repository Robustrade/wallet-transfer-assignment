package domain

import (
	"errors"
	"fmt"
	"time"
)

// Status is the transfer state machine.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusProcessed Status = "PROCESSED"
	StatusFailed    Status = "FAILED"
)

// Failure reasons persisted on FAILED transfers.
const (
	ReasonInsufficientFunds = "INSUFFICIENT_FUNDS"
)

// Transfer is one wallet-to-wallet movement attempt.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         Status
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewPendingTransfer builds a transfer in PENDING after validating inputs.
func NewPendingTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount int64, now time.Time) (Transfer, error) {
	details := make([]FieldError, 0, 4)
	if err := ValidateIdempotencyKey(idempotencyKey); err != nil {
		details = append(details, validationDetails(err, "idempotencyKey")...)
	}
	if err := ValidateWalletID(fromWalletID); err != nil {
		details = append(details, validationDetails(err, "fromWalletId")...)
	}
	if err := ValidateWalletID(toWalletID); err != nil {
		details = append(details, validationDetails(err, "toWalletId")...)
	}
	if fromWalletID != "" && fromWalletID == toWalletID {
		details = append(details, FieldError{Field: "toWalletId", Message: "must be different from fromWalletId"})
	}
	if err := ValidateTransferAmount(amount); err != nil {
		details = append(details, validationDetails(err, "")...)
	}
	if id == "" {
		details = append(details, FieldError{Field: "id", Message: "is required"})
	}
	if len(details) > 0 {
		return Transfer{}, &ValidationError{Details: details}
	}
	now = now.UTC()
	return Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// MarkProcessed transitions PENDING to PROCESSED.
func (t Transfer) MarkProcessed(now time.Time) (Transfer, error) {
	if t.Status != StatusPending {
		return Transfer{}, fmt.Errorf("cannot process transfer in status %s: %w", t.Status, ErrInvalidTransition)
	}
	t.Status = StatusProcessed
	t.FailureReason = ""
	t.UpdatedAt = now.UTC()
	return t, nil
}

// MarkFailed transitions PENDING to FAILED.
func (t Transfer) MarkFailed(reason string, now time.Time) (Transfer, error) {
	if t.Status != StatusPending {
		return Transfer{}, fmt.Errorf("cannot fail transfer in status %s: %w", t.Status, ErrInvalidTransition)
	}
	if reason == "" {
		return Transfer{}, &ValidationError{Details: []FieldError{{
			Field:   "failureReason",
			Message: "is required when failing a transfer",
		}}}
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	t.UpdatedAt = now.UTC()
	return t, nil
}

func validationDetails(err error, field string) []FieldError {
	var validation *ValidationError
	if !errors.As(err, &validation) {
		return []FieldError{{Field: field, Message: err.Error()}}
	}
	if field == "" {
		return append([]FieldError(nil), validation.Details...)
	}
	return retarget(validation.Details, field)
}

func retarget(details []FieldError, field string) []FieldError {
	out := make([]FieldError, len(details))
	for i, detail := range details {
		detail.Field = field
		out[i] = detail
	}
	return out
}
