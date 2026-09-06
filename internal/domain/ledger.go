package domain

import "time"

type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}
