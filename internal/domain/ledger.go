package domain

import (
	"time"

	"github.com/google/uuid"
)

// EntryType is the side of a double-entry ledger record.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one immutable side of a transfer.
type LedgerEntry struct {
	ID         int64
	TransferID uuid.UUID
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

// LedgerEntriesFor returns the two entries that record a processed transfer:
// a DEBIT on the source wallet and a CREDIT on the destination.
func LedgerEntriesFor(t *Transfer, now time.Time) []LedgerEntry {
	return []LedgerEntry{
		{TransferID: t.ID, WalletID: t.FromWalletID, Type: EntryDebit, Amount: t.Amount, CreatedAt: now},
		{TransferID: t.ID, WalletID: t.ToWalletID, Type: EntryCredit, Amount: t.Amount, CreatedAt: now},
	}
}
