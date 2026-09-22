package domain

import "time"

// LedgerEntryType identifies whether a ledger entry increases or decreases
// a wallet's balance.
type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry is a single half of a double-entry bookkeeping record.
// Every processed transfer produces exactly two entries: a debit against
// the source wallet and a credit against the destination wallet, both
// referencing the same TransferID and sharing the same Amount so the
// ledger always balances.
type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}
