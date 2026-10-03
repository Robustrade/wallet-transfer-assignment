package domain

import "time"

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64
	TransferID int64
	WalletID   string
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}
