package domain

import "time"

type Wallet struct {
	WalletID  string    `db:"wallet_id"`
	Balance   string    `db:"balance"` // Using string for NUMERIC to preserve precision
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Transfer struct {
	TransferID     string    `db:"transfer_id"`
	IdempotencyKey string    `db:"idempotency_key"`
	FromWalletID   string    `db:"from_wallet_id"`
	ToWalletID     string    `db:"to_wallet_id"`
	Amount         string    `db:"amount"` // Using string for NUMERIC to preserve precision
	State          string    `db:"state"`  // PENDING, PROCESSED, FAILED
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type LedgerEntry struct {
	EntryID    string    `db:"entry_id"`
	WalletID   string    `db:"wallet_id"`
	TransferID string    `db:"transfer_id"`
	Type       string    `db:"type"` // DEBIT or CREDIT
	Amount     string    `db:"amount"`
	CreatedAt  time.Time `db:"created_at"`
}

type IdempotencyRecord struct {
	IdempotencyKey  string    `db:"idempotency_key"`
	TransferID      string    `db:"transfer_id"`
	ResponsePayload string    `db:"response_payload"` // JSON
	CreatedAt       time.Time `db:"created_at"`
}

// Transfer request and response DTOs
type TransferRequest struct {
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
	FromWalletID   string `json:"from_wallet_id" binding:"required"`
	ToWalletID     string `json:"to_wallet_id" binding:"required"`
	Amount         string `json:"amount" binding:"required"`
}

type TransferResponse struct {
	TransferID string `json:"transfer_id"`
	State      string `json:"state"`
	Message    string `json:"message"`
}
