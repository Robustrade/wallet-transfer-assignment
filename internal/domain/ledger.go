package domain

import "time"

type EntryType string

const (
	DEBIT  EntryType = "DEBIT"
	CREDIT EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	EntryType  EntryType
	Amount     int64
	CreatedAt  time.Time
}
