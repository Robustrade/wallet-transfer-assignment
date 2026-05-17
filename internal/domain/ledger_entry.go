package domain

import "time"

type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}
