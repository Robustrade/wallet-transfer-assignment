package domain

import "time"

// EntryType represents whether a ledger entry is a debit or credit.
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// LedgerEntry represents one side of a double-entry ledger record.
// Every successful (PROCESSED) transfer produces exactly two entries: one DEBIT and one CREDIT. Failed transfers write no entries.
type LedgerEntry struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	EntryType  EntryType `json:"entryType"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}
