package domain

import (
	"errors"
	"time"
)

type Status string

const (
	Pending   Status = "PENDING"
	Processed Status = "PROCESSED"
	Failed    Status = "FAILED"
)

var (
	ErrInvalidRequest    = errors.New("invalid transfer request")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrIdempotencyReuse  = errors.New("idempotency key was already used with a different request")
)

type CreateRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}
type Transfer struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Status         Status    `json:"status"`
	FailureReason  string    `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (r CreateRequest) Validate() error {
	if r.IdempotencyKey == "" || len(r.IdempotencyKey) > 255 || r.FromWalletID == "" || r.ToWalletID == "" || r.FromWalletID == r.ToWalletID || r.Amount <= 0 {
		return ErrInvalidRequest
	}
	return nil
}
