package model

import "time"

type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// LedgerEntry represents an immutable double-entry record of money movement.
type LedgerEntry struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	EntryType  EntryType `json:"entryType"` // DEBIT or CREDIT
	Amount     int64     `json:"amount"`    // Strictly positive amount
	CreatedAt  time.Time `json:"createdAt"`
}
