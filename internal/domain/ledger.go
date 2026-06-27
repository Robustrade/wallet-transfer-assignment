package domain

import "time"

// EntryType is the direction of a ledger entry.
type EntryType string

const (
	// EntryDebit decreases the associated wallet's balance.
	EntryDebit EntryType = "DEBIT"
	// EntryCredit increases the associated wallet's balance.
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one line of the double-entry ledger. Every processed transfer
// produces exactly two entries: a DEBIT on the source wallet and a CREDIT on
// the destination wallet, with equal amounts, so the ledger always balances.
type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       EntryType
	Amount     Money
	CreatedAt  time.Time
}

// NewDoubleEntry builds the balanced debit/credit pair for a transfer. Keeping
// the construction in the domain guarantees the two entries are always created
// together and reference the same transfer and amount.
func NewDoubleEntry(debitID, creditID, transferID, fromWalletID, toWalletID string, amount Money) []LedgerEntry {
	now := time.Now().UTC()
	return []LedgerEntry{
		{
			ID:         debitID,
			WalletID:   fromWalletID,
			TransferID: transferID,
			Type:       EntryDebit,
			Amount:     amount,
			CreatedAt:  now,
		},
		{
			ID:         creditID,
			WalletID:   toWalletID,
			TransferID: transferID,
			Type:       EntryCredit,
			Amount:     amount,
			CreatedAt:  now,
		},
	}
}
