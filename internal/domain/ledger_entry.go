package domain

import "time"

type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

// NewLedgerEntryPair builds the two ledger entries (debit + credit) for a transfer.
// Keeping this as a single constructor guarantees the double-entry invariant —
// you can't create one without the other.
func NewLedgerEntryPair(transferID, fromWalletID, toWalletID string, amount int64) (debit, credit *LedgerEntry) {
	debit = &LedgerEntry{
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       LedgerEntryTypeDebit,
		Amount:     amount,
	}
	credit = &LedgerEntry{
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       LedgerEntryTypeCredit,
		Amount:     amount,
	}
	return debit, credit
}
