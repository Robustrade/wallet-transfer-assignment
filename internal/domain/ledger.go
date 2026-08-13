package domain

import "time"

type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

// DoubleEntry builds the balanced pair of ledger rows for a processed
// transfer: a debit on the source wallet and a credit on the destination,
// both for the same amount.
func DoubleEntry(debitID, creditID, transferID, fromWalletID, toWalletID string, amount int64) [2]LedgerEntry {
	return [2]LedgerEntry{
		{ID: debitID, TransferID: transferID, WalletID: fromWalletID, EntryType: EntryDebit, Amount: amount},
		{ID: creditID, TransferID: transferID, WalletID: toWalletID, EntryType: EntryCredit, Amount: amount},
	}
}
