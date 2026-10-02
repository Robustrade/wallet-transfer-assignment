package domain

import "time"

type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}
