package domain

import "time"

// EntryType distinguishes the two sides of a double-entry ledger record.
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// LedgerEntry is one side of a double-entry bookkeeping record for a
// transfer. Every PROCESSED transfer must produce exactly one DEBIT entry
// (on the source wallet) and one CREDIT entry (on the destination wallet),
// both referencing the same transfer and carrying the same amount.
type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	EntryType  EntryType
	Amount     int64
	CreatedAt  time.Time
}
