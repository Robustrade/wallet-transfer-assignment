package domain

import (
	"time"

	"github.com/google/uuid"
)

type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64
	TransferID uuid.UUID
	WalletID   string
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

// LedgerEntriesForTransfer builds the balanced debit/credit pair for a
// processed transfer.
func LedgerEntriesForTransfer(t *Transfer) []LedgerEntry {
	return []LedgerEntry{
		{TransferID: t.ID, WalletID: t.FromWalletID, EntryType: EntryDebit, Amount: t.Amount},
		{TransferID: t.ID, WalletID: t.ToWalletID, EntryType: EntryCredit, Amount: t.Amount},
	}
}
