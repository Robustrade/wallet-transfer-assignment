package domain

import (
	"time"
)

// LedgerEntryType denotes whether a ledger entry is a debit or a credit.
type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry represents an immutable double-entry ledger record.
type LedgerEntry struct {
	ID           string          `gorm:"primaryKey;size:64" json:"id"`
	WalletID     string          `gorm:"size:64;not null;index" json:"walletId"`
	TransferID   string          `gorm:"size:64;not null;index" json:"transferId"`
	Type         LedgerEntryType `gorm:"size:10;not null" json:"type"`
	Amount       int64           `gorm:"not null;check:amount > 0" json:"amount"`
	BalanceAfter int64           `gorm:"not null" json:"balanceAfter"`
	CreatedAt    time.Time       `gorm:"autoCreateTime" json:"createdAt"`
}

// TableName overrides the default table name in GORM.
func (LedgerEntry) TableName() string {
	return "ledger_entries"
}
