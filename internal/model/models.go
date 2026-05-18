package model

import (
	"encoding/json"
	"time"
)

type Wallet struct {
	ID        string    `json:"id"`
	Balance   float64   `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

type Transfer struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotency_key"`
	FromWalletID   string    `json:"from_wallet_id"`
	ToWalletID     string    `json:"to_wallet_id"`
	Amount         float64   `json:"amount"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

type LedgerEntry struct {
	ID         int       `json:"id"`
	TransferID string    `json:"transfer_id"`
	WalletID   string    `json:"wallet_id"`
	Type       string    `json:"type"`
	Amount     float64   `json:"amount"`
	CreatedAt  time.Time `json:"created_at"`
}

type IdempotencyKey struct {
	IdempotencyKey string          `json:"idempotency_key"`
	LockedAt       time.Time       `json:"locked_at"`
	ResponseCode   *int            `json:"response_code"`
	ResponseBody   json.RawMessage `json:"response_body"`
}

type CreateTransferRequest struct {
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         float64 `json:"amount"`
}
