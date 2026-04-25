package model

import "time"

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

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
