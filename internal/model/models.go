package model

import "time"

// Transfer represents a wallet transfer record
type Transfer struct {
	ID             int64      `gorm:"column:id;primaryKey" json:"id"`
	FromWalletID   int64      `gorm:"column:from_wallet_id;index" json:"from_wallet_id"`
	ToWalletID     int64      `gorm:"column:to_wallet_id;index" json:"to_wallet_id"`
	Amount         int64      `gorm:"column:amount" json:"amount"`
	State          string     `gorm:"column:state" json:"state"`
	IdempotencyKey string     `gorm:"column:idempotency_key;uniqueIndex" json:"idempotency_key"`
	CreatedAt      time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt      *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for Transfer
func (Transfer) TableName() string {
	return "transfers"
}

// Wallet represents a wallet account
type Wallet struct {
	ID        int64      `gorm:"column:id;primaryKey" json:"id"`
	Balance   int64      `gorm:"column:balance" json:"balance"`
	CreatedAt time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for Wallet
func (Wallet) TableName() string {
	return "wallets"
}

// LedgerEntry represents a debit or credit ledger entry
type LedgerEntry struct {
	ID         int64      `gorm:"column:id;primaryKey" json:"id"`
	WalletID   int64      `gorm:"column:wallet_id;index" json:"wallet_id"`
	TransferID int64      `gorm:"column:transfer_id;index" json:"transfer_id"`
	Type       string     `gorm:"column:type" json:"type"` // DEBIT or CREDIT
	Amount     int64      `gorm:"column:amount" json:"amount"`
	CreatedAt  time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt  *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for LedgerEntry
func (LedgerEntry) TableName() string {
	return "ledger_entries"
}

// IdempotencyRecord stores idempotency state
type IdempotencyRecord struct {
	IdempotencyKey  string     `gorm:"column:idempotency_key;primaryKey" json:"idempotency_key"`
	RequestHash     string     `gorm:"column:request_hash" json:"request_hash"`
	Status          string     `gorm:"column:status" json:"status"`
	ResponsePayload string     `gorm:"column:response_payload" json:"response_payload"`
	StatusCode      int        `gorm:"column:status_code" json:"status_code"`
	CreatedAt       time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt       *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for IdempotencyRecord
func (IdempotencyRecord) TableName() string {
	return "idempotency_records"
}

// TransferRequest represents the transfer request (kept for backward compatibility)
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   int64
	ToWalletID     int64
	Amount         int64
}

// TransferResponse represents the transfer response (kept for backward compatibility)
type TransferResponse struct {
	TransferID int64
	Status     string
}
