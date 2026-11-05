package domain

import "time"

// Wallet holds a balance in minor units. The balance is never negative
// (enforced by a DB CHECK constraint).
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// LedgerEntryType is the side of a double-entry record.
type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry is one side of a transfer. Every PROCESSED transfer has exactly
// one DEBIT on the source wallet and one CREDIT on the destination wallet.
type LedgerEntry struct {
	ID         int64           `json:"id"`
	TransferID string          `json:"transferId"`
	WalletID   string          `json:"walletId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"`
	CreatedAt  time.Time       `json:"createdAt"`
}
