package domain

import "time"

// LedgerEntryType is DEBIT or CREDIT.
type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry is one side of a double-entry transfer record.
type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}
