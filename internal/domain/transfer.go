package domain

import (
	"errors"
	"time"
)

type TransferState string

const (
	TransferStatePending   TransferState = "PENDING"
	TransferStateProcessed TransferState = "PROCESSED"
	TransferStateFailed    TransferState = "FAILED"
)

var (
	ErrDuplicateTransfer = errors.New("duplicate transfer")
	ErrInvalidAmount     = errors.New("invalid transfer amount")
	ErrSameWallet        = errors.New("source and destination wallets must be different")
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	State          TransferState
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
