package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID             uuid.UUID      `json:"transferId"`
	IdempotencyKey *string        `json:"idempotencyKey,omitempty"`
	FromWalletID   uuid.UUID      `json:"fromWalletId"`
	ToWalletID     uuid.UUID      `json:"toWalletId"`
	Amount         Money          `json:"amount"`
	Status         TransferStatus `json:"status"`
	ErrorCode      *string        `json:"errorCode,omitempty"`
	ErrorMessage   *string        `json:"errorMessage,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	ProcessedAt    *time.Time     `json:"processedAt,omitempty"`
}

func NewTransfer(id uuid.UUID, idempotencyKey *string, fromWalletID, toWalletID uuid.UUID, amount Money) (*Transfer, error) {
	if fromWalletID == toWalletID {
		return nil, ErrSameWalletTransfer()
	}
	if !amount.IsPositive() {
		return nil, ErrInvalidAmount("transfer amount must be strictly greater than zero")
	}
	now := time.Now().UTC()
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         TransferStatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (t *Transfer) MarkProcessed() error {
	if t.Status != TransferStatusPending {
		return ErrInvalidStateTransition(string(t.Status), string(TransferStatusProcessed))
	}
	now := time.Now().UTC()
	t.Status = TransferStatusProcessed
	t.ProcessedAt = &now
	t.UpdatedAt = now
	return nil
}

func (t *Transfer) MarkFailed(errCode ErrorCode, errMsg string) error {
	if t.Status != TransferStatusPending {
		return ErrInvalidStateTransition(string(t.Status), string(TransferStatusFailed))
	}
	now := time.Now().UTC()
	codeStr := string(errCode)
	t.Status = TransferStatusFailed
	t.ErrorCode = &codeStr
	t.ErrorMessage = &errMsg
	t.UpdatedAt = now
	return nil
}
