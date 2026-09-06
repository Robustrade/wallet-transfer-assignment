package domain

import (
	"errors"
	"time"
)

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

var (
	ErrInvalidAmount       = errors.New("amount must be a positive integer")
	ErrEmptyIdempotencyKey = errors.New("idempotencyKey is required")
	ErrSameWallet          = errors.New("fromWalletId and toWalletId must differ")
	ErrEmptyWalletID       = errors.New("fromWalletId and toWalletId are required")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
)

// Transfer is the domain representation of a wallet-to-wallet transfer.
// Amount is denominated in the smallest currency unit (e.g. cents).
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateTransferRequest is the validated input to the transfer service.
type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Validate enforces request-shape invariants that don't require DB access.
// Wallet existence and balance sufficiency are checked by the service, since
// they require a DB round trip inside the transaction.
func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return ErrEmptyIdempotencyKey
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return ErrEmptyWalletID
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
