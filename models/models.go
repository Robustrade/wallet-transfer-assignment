package models

import (
	"time"
	"wallet-transfer-assigment/utils"
)

// wallets
// transfers
// ledger_entries
// idempotency_records

type Wallet struct {
	WalletID  string    `json:"wallet_id"`
	Balance   int64     `json:"balance"` // in lowest denomination (e.g., cents for USD)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Transfers struct {
	TransactionID string                `json:"transaction_id"`
	FromWalletID  string                `json:"from_wallet_id"`
	ToWalletID    string                `json:"to_wallet_id"`
	Amount        int64                 `json:"amount"` // in lowest denomination (e.g., cents for USD)
	Status        utils.TRANSFER_STATUS `json:"status"` // e.g., "PENDING", "COMPLETED", "FAILED"
	Comments      string                `json:"comments"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

type LedgerEntries struct {
	WalletID      string                 `json:"wallet_id"`
	Amount        int64                  `json:"amount"` // in lowest denomination (e.g., cents for USD)
	Type          utils.TRANSACTION_TYPE `json:"type"`
	TransactionID string                 `json:"transaction_id"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type IdempotencyRecords struct {
	IdempotencyKey string    `json:"idempotency_key"`
	TransactionID  string    `json:"transaction_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Response struct {
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Status  string      `json:"status"`
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" validate:"required"`
	FromWalletID   string `json:"fromWalletId" validate:"required"`
	ToWalletID     string `json:"toWalletId" validate:"required"`
	Amount         int64  `json:"amount" validate:"required,gt=0"`
}

type Statement struct {
	CreatedAt     time.Time `json:"created_at"`
	TransactionID string    `json:"transaction_id"`
	EntryID       string    `json:"entry_id"`
	Amount        int64     `json:"amount"`
	EntryType     string    `json:"entry_type"`
	BalanceAfter  int64     `json:"balance_after"`
	FromWalletID  string    `json:"from_wallet_id"`
	ToWalletID    string    `json:"to_wallet_id"`
	Status        string    `json:"status"`
	Comments      string    `json:"comments"`
}

type WalletRequest struct {
	Balance int64 `json:"balance" validate:"required,gt=0"`
}
