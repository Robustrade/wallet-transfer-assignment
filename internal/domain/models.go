package domain

import (
	"time"
)

// Supported currency constant. The service supports INR only.
const (
	CurrencyINR = "INR"
)

// Transfer status constants representing the state machine.
const (
	TransferStatusPending   = "PENDING"
	TransferStatusProcessed = "PROCESSED"
	TransferStatusFailed    = "FAILED"
)

// Ledger entry types for double-entry bookkeeping.
const (
	LedgerEntryDebit  = "DEBIT"
	LedgerEntryCredit = "CREDIT"
)

// Failure codes for transfers.
const (
	FailureCodeInsufficientFunds = "INSUFFICIENT_FUNDS"
	FailureCodeSystemError       = "SYSTEM_ERROR"
)

// User represents an account holder. Authentication and user management
// are out of scope for this service, but users anchor wallet ownership.
type User struct {
	ID        string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	Name      string    `gorm:"type:varchar(255);not null" json:"name"`
	CreatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"createdAt"`
}

// Wallet represents a customer wallet.
// Balance is stored as integer paise (1 INR = 100 paise) to prevent floating-point errors.
type Wallet struct {
	ID        string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	UserID    string    `gorm:"type:varchar(64);not null;index" json:"userId"`
	Balance   int64     `gorm:"type:bigint;not null;default:0" json:"balance"` // in paise
	Currency  string    `gorm:"type:varchar(3);not null;default:'INR'" json:"currency"`
	CreatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"createdAt"`
	UpdatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updatedAt"`

	User *User `gorm:"foreignKey:UserID;references:ID" json:"-"`
}

// Transfer represents a wallet-to-wallet funds transfer request.
type Transfer struct {
	ID           string    `gorm:"primaryKey;type:varchar(64)" json:"id"`
	FromWalletID string    `gorm:"type:varchar(64);not null;index" json:"fromWalletId"`
	ToWalletID   string    `gorm:"type:varchar(64);not null;index" json:"toWalletId"`
	Amount       int64     `gorm:"type:bigint;not null" json:"amount"` // in paise
	Currency     string    `gorm:"type:varchar(3);not null;default:'INR'" json:"currency"`
	Status       string    `gorm:"type:varchar(32);not null;index" json:"status"`
	FailureCode  *string   `gorm:"type:varchar(64)" json:"failureCode,omitempty"`
	CreatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP;index" json:"createdAt"`
	UpdatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updatedAt"`

	FromWallet *Wallet `gorm:"foreignKey:FromWalletID;references:ID" json:"-"`
	ToWallet   *Wallet `gorm:"foreignKey:ToWalletID;references:ID" json:"-"`
}

// LedgerEntry represents an immutable financial entry in double-entry bookkeeping.
// Every successful transfer generates exactly two entries: one DEBIT and one CREDIT.
type LedgerEntry struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TransferID string    `gorm:"type:varchar(64);not null;index" json:"transferId"`
	WalletID   string    `gorm:"type:varchar(64);not null;index" json:"walletId"`
	EntryType  string    `gorm:"type:varchar(16);not null" json:"entryType"` // DEBIT or CREDIT
	Amount     int64     `gorm:"type:bigint;not null" json:"amount"`         // in paise
	CreatedAt  time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"createdAt"`

	Transfer *Transfer `gorm:"foreignKey:TransferID;references:ID" json:"-"`
	Wallet   *Wallet   `gorm:"foreignKey:WalletID;references:ID" json:"-"`
}

// IdempotencyRecord persists incoming request fingerprints and completed responses.
// This guarantees exactly-once execution semantics and prevents duplicate debits across retries.
type IdempotencyRecord struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	IdempotencyKey string     `gorm:"type:varchar(255);not null;uniqueIndex" json:"idempotencyKey"`
	RequestHash    string     `gorm:"type:varchar(64);not null" json:"requestHash"`
	TransferID     *string    `gorm:"type:varchar(64)" json:"transferId,omitempty"`
	ResponseStatus int        `gorm:"not null;default:0" json:"responseStatus"`
	ResponseBody   string     `gorm:"type:text;not null;default:''" json:"responseBody"`
	CreatedAt      time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP" json:"createdAt"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}
