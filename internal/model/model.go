package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// TransferState defines the lifecycle state of a transfer.
type TransferState string

const (
	TransferStatePending   TransferState = "PENDING"
	TransferStateProcessed TransferState = "PROCESSED"
	TransferStateFailed    TransferState = "FAILED"
)

// EntryType defines whether a ledger entry is a debit or credit.
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// Wallet represents the balance and metadata for a wallet.
// The balance CHECK constraint is a database-level backstop against double spend.
type Wallet struct {
	WalletID  string    `gorm:"primaryKey;column:wallet_id"`
	Balance   int64     `gorm:"column:balance;not null;check:balance >= 0"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// Transfer represents a transfer workflow record.
type Transfer struct {
	TransferID   string        `gorm:"primaryKey;column:transfer_id"`
	FromWalletID string        `gorm:"column:from_wallet_id;not null"`
	ToWalletID   string        `gorm:"column:to_wallet_id;not null"`
	Amount       int64         `gorm:"column:amount;not null"`
	State        TransferState `gorm:"column:state;not null"`
	CreatedAt    time.Time     `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time     `gorm:"column:updated_at;autoUpdateTime"`
}

// LedgerEntry records an individual debit or credit for a transfer.
// The unique (transfer_id, type) index guarantees each transfer produces at most
// one debit and one credit, enforcing double-entry integrity at the database.
type LedgerEntry struct {
	LedgerEntryID string    `gorm:"primaryKey;column:ledger_entry_id"`
	WalletID      string    `gorm:"column:wallet_id;not null;index"`
	TransferID    string    `gorm:"column:transfer_id;not null;uniqueIndex:ux_ledger_transfer_type"`
	EntryType     EntryType `gorm:"column:type;not null;uniqueIndex:ux_ledger_transfer_type"`
	Amount        int64     `gorm:"column:amount;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
}

// IdempotencyRecord maps a client idempotency key to a transfer and stores a
// snapshot of the original response. Storing the response guarantees that a
// replayed request returns the exact original result, even if wallet balances
// have since changed, delivering exactly-once semantics at the API level.
type IdempotencyRecord struct {
	IdempotencyKey string    `gorm:"primaryKey;column:idempotency_key"`
	TransferID     string    `gorm:"column:transfer_id;not null"`
	Response       string    `gorm:"column:response;type:text;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
}

// NewID returns a randomly generated hex identifier.
func NewID() string {
	var buffer [16]byte
	_, _ = rand.Read(buffer[:])
	return hex.EncodeToString(buffer[:])
}
