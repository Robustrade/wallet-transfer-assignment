package model

import (
	"errors"
	"time"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

var (
	ErrInvalidAmount         = errors.New("invalid amount")
	ErrSameWallet            = errors.New("from and to wallet cannot be same")
	ErrMissingWalletID       = errors.New("wallet ids are required")
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrTransferFailedError   = errors.New("transfer failed")
	ErrTransferProcessed     = errors.New("transfer already processed")
	ErrTransferPending       = errors.New("transfer is still pending, please check back later")
)

type TransferRequest struct {
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
	FromWalletId   string `json:"from_wallet_id" binding:"required"`
	ToWalletId     string `json:"to_wallet_id" binding:"required"`
	Amount         int64  `json:"amount" binding:"required,gt=0"`
}

type Transfer struct {
	Id             string         `json:"id" db:"id"`
	IdempotencyKey string         `json:"idempotency_key" db:"idempotency_key"`
	FromWalletId   string         `json:"from_wallet_id" db:"from_wallet_id"`
	ToWalletId     string         `json:"to_wallet_id" db:"to_wallet_id"`
	Amount         int64          `json:"amount" db:"amount"`
	Status         TransferStatus `json:"status" db:"status"`
	CreatedAt      time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at" db:"updated_at"`
}

type Wallet struct {
	Id        string    `json:"id" db:"id"`
	Balance   int64     `json:"balance" db:"balance"`
	Version   int64     `json:"version" db:"version"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type Ledger struct {
	Id         string    `json:"id" db:"id"`
	TransferId string    `json:"transfer_id" db:"transfer_id"`
	WalletId   string    `json:"wallet_id" db:"wallet_id"`
	Amount     int64     `json:"amount" db:"amount"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type OutboxEntry struct {
	Id             string         `json:"id" db:"id"`
	TransferId     string         `json:"transfer_id" db:"transfer_id"`
	EventType      string         `json:"event_type" db:"event_type"`
	EventStatus    string         `json:"event_status" db:"event_status"`
	Payload        string         `json:"payload" db:"payload"`
	TransferStatus TransferStatus `json:"transfer_status" db:"transfer_status"`
	CreatedAt      time.Time      `json:"created_at" db:"created_at"`
}

type TransferPayload struct {
	TransferID   string `json:"transfer_id"`
	FromWalletID string `json:"from_wallet_id"`
	ToWalletID   string `json:"to_wallet_id"`
	Amount       int64  `json:"amount"`
}

type LedgerEntry struct {
	Id         string    `json:"id" db:"id"`
	TransferId string    `json:"transfer_id" db:"transfer_id"`
	WalletId   string    `json:"wallet_id" db:"wallet_id"`
	Amount     int64     `json:"amount" db:"amount"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}
