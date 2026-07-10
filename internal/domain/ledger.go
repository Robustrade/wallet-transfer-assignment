package domain

import "time"

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	EntryType  LedgerEntryType
	Amount     float64
	CreatedAt  time.Time
}
