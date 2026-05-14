package models

import "time"

// LedgerEntry represents a double-entry ledger record
type LedgerEntry struct {
	EntryID    string    `gorm:"primaryKey;column:entry_id" json:"entry_id"`
	WalletID   string    `gorm:"column:wallet_id;index:idx_ledger_wallet_transfer,priority:1" json:"wallet_id"`
	TransferID string    `gorm:"column:transfer_id;index:idx_ledger_wallet_transfer,priority:2" json:"transfer_id"`
	Type       string    `gorm:"column:type" json:"type"`
	Amount     int64     `gorm:"column:amount" json:"amount"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
}

func (LedgerEntry) TableName() string {
	return "ledger_entries"
}

// Entry types
const (
	EntryTypeDebit  = "DEBIT"
	EntryTypeCredit = "CREDIT"
)
