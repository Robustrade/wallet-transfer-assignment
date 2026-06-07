package domain

import "time"

// LedgerEntryType is the direction of a ledger entry.
type LedgerEntryType string

// Ledger entry directions for double-entry bookkeeping.
const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry is a single line in the double-entry ledger. Every processed
// transfer produces exactly one DEBIT (source wallet) and one CREDIT
// (destination wallet) of equal amount, so the ledger always balances.
type LedgerEntry struct {
	ID         int64
	TransferID string
	WalletID   string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

// NewLedgerPair builds the balanced debit/credit pair for a processed transfer.
// Keeping pair construction in the domain guarantees that the two entries are
// always created together and with the same amount.
func NewLedgerPair(transferID, fromWalletID, toWalletID string, amount int64) (debit, credit LedgerEntry) {
	now := time.Now().UTC()
	debit = LedgerEntry{
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       EntryDebit,
		Amount:     amount,
		CreatedAt:  now,
	}
	credit = LedgerEntry{
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       EntryCredit,
		Amount:     amount,
		CreatedAt:  now,
	}
	return debit, credit
}
