package domain

import "errors"

var (
	ErrInvalidRequest      = errors.New("idempotency key, distinct wallet IDs, and a positive amount are required")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrWalletNotFound      = errors.New("wallet not found")
)

type TransferStatus string

const (
	Pending   TransferStatus = "PENDING"
	Processed TransferStatus = "PROCESSED"
	Failed    TransferStatus = "FAILED"
)

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" || r.FromWalletID == "" || r.ToWalletID == "" || r.FromWalletID == r.ToWalletID || r.Amount <= 0 {
		return ErrInvalidRequest
	}
	return nil
}

type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	FailureReason  string         `json:"failureReason,omitempty"`
	Replayed       bool           `json:"replayed"`
}
