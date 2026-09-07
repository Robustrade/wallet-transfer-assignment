package model

import "time"

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

func (s TransferStatus) IsTerminal() bool {
	return s == TransferStatusProcessed || s == TransferStatusFailed
}

// Transfer is the internal domain entity
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotency_key"`
	FromWalletID   string         `json:"from_wallet_id"`
	ToWalletID     string         `json:"to_wallet_id"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	FailureReason  *string        `json:"failure_reason,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// CreateTransferRequest is the v1 API request schema
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required,min=8,max=128"`
	FromWalletID   string `json:"fromWalletId"   binding:"required,min=3,max=64,wallet_id"`
	ToWalletID     string `json:"toWalletId"     binding:"required,min=3,max=64,wallet_id,nefield=FromWalletID"`
	Amount         string `json:"amount"         binding:"required,amount_positive"`
}

// CreateTransferResponse is the v1 API response schema (omits timestamps, formatted amount)
type CreateTransferResponse struct {
	TransferID   string         `json:"transferId"`
	Status       TransferStatus `json:"status"`
	FromWalletID string         `json:"fromWalletId"`
	ToWalletID   string         `json:"toWalletId"`
	Amount       string         `json:"amount"`
}

// StatementEntry represents a line item on the statement, carrying the financial transaction date
type StatementEntry struct {
	TransferID      string         `json:"transferId"`
	Type            string         `json:"type"` // "DEBIT" or "CREDIT"
	CounterpartID   string         `json:"counterpartId"`
	Amount          string         `json:"amount"`
	BalanceBefore   string         `json:"balanceBefore"`
	BalanceAfter    string         `json:"balanceAfter"`
	Status          TransferStatus `json:"status"`
	TransactionDate time.Time      `json:"transactionDate"`
}

// StatementResponse is the payload returned for statement inquiries
type StatementResponse struct {
	WalletID     string           `json:"walletId"`
	Balance      string           `json:"balance"` // Formatted e.g. "100.50"
	Transactions []StatementEntry `json:"transactions"`
}
