package domain

import "time"

// LedgerEntryType constants for double-entry bookkeeping.
const (
	LedgerEntryTypeDebit  = "DEBIT"
	LedgerEntryTypeCredit = "CREDIT"
)

// LedgerEntry represents a single entry in the double-entry ledger.
type LedgerEntry struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	EntryType  string    `json:"entryType"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}
