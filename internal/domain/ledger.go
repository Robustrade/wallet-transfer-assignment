package domain

import "github.com/google/uuid"

// EntryType is the side of a double-entry ledger record.
type EntryType string

// Ledger entry sides.
const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one immutable side of a transfer in the double-entry ledger.
type LedgerEntry struct {
	TransferID uuid.UUID
	WalletID   string
	Type       EntryType
	Amount     int64
}

// LedgerEntries returns the balanced DEBIT/CREDIT pair for a processed transfer.
// Pending and failed transfers move no money and therefore have no entries.
func (t Transfer) LedgerEntries() ([]LedgerEntry, error) {
	if t.Status != StatusProcessed {
		return nil, ErrInvalidTransition
	}
	return []LedgerEntry{
		{TransferID: t.ID, WalletID: t.FromWalletID, Type: EntryDebit, Amount: t.Amount},
		{TransferID: t.ID, WalletID: t.ToWalletID, Type: EntryCredit, Amount: t.Amount},
	}, nil
}
