package domain

import (
	"errors"

	"github.com/google/uuid"
)

var (
	ErrInvalidRequest      = errors.New("idempotency key, distinct wallet IDs, and a positive amount are required")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrWalletNotFound      = errors.New("wallet not found")
)

// TransferStatus represents the immutable outcome of a transfer workflow.
type TransferStatus string

const (
	Pending   TransferStatus = "PENDING"
	Processed TransferStatus = "PROCESSED"
	Failed    TransferStatus = "FAILED"
)

// CreateTransferRequest contains the client input required to move funds once.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// Validate rejects malformed requests before they reach the persistence layer.
func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" || r.FromWalletID == "" || r.ToWalletID == "" || r.FromWalletID == r.ToWalletID || r.Amount <= 0 {
		return ErrInvalidRequest
	}
	if err := ValidateWalletID(r.FromWalletID); err != nil {
		return err
	}
	if err := ValidateWalletID(r.ToWalletID); err != nil {
		return ErrInvalidRequest
	}
	return nil
}

// ValidateWalletID confirms that an externally supplied wallet ID is a UUID.
func ValidateWalletID(walletID string) error {
	if _, err := uuid.Parse(walletID); err != nil {
		return ErrInvalidRequest
	}
	return nil
}

// Transfer is the result returned for both initial requests and idempotent replays.
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
