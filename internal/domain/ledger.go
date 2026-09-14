package domain

import "time"

// EntryType is the side of the ledger an entry falls on.
type EntryType string

const (
	// EntryDebit removes money from a wallet.
	EntryDebit EntryType = "DEBIT"

	// EntryCredit adds money to a wallet.
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one side of a transfer. Entries are never amended: a
// correction would be recorded as a further transfer.
type LedgerEntry struct {
	ID         int64
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

// LedgerEntriesFor builds the double-entry pair a transfer must produce: the
// source wallet is debited and the destination credited, for the same amount.
//
// Returning both together is what makes "exactly two entries, and they balance"
// a property of construction rather than something each caller has to remember.
func LedgerEntriesFor(t Transfer) []LedgerEntry {
	return []LedgerEntry{
		{
			TransferID: t.ID,
			WalletID:   t.FromWalletID,
			Type:       EntryDebit,
			Amount:     t.Amount,
		},
		{
			TransferID: t.ID,
			WalletID:   t.ToWalletID,
			Type:       EntryCredit,
			Amount:     t.Amount,
		},
	}
}
