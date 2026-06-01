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
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrInvalidAmount     = errors.New("amount must be positive")
	ErrSameWallet        = errors.New("cannot transfer to the same wallet")
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrTransferNotFound  = errors.New("transfer not found")
)

type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Transfer struct {
	ID             string        `json:"id"`
	IdempotencyKey string        `json:"idempotency_key"`
	FromWalletID   string        `json:"from_wallet_id"`
	ToWalletID     string        `json:"to_wallet_id"`
	Amount         int64         `json:"amount"`
	State          TransferState `json:"state"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type LedgerEntry struct {
	ID         int64     `json:"id"`
	WalletID   string    `json:"wallet_id"`
	TransferID string    `json:"transfer_id"`
	EntryType  string    `json:"entry_type"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"created_at"`
}

func (t *Transfer) CanTransitionTo(state TransferState) bool {
	if t.State == TransferStatePending {
		return state == TransferStateProcessed || state == TransferStateFailed
	}
	return false
}
