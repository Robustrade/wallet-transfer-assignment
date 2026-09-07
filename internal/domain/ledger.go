package domain

import "time"

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64
	TransferID string
	WalletID   string
	EntryType  EntryType
	Amount     int64
	CreatedAt  time.Time
}

// DoubleEntry builds the balanced DEBIT/CREDIT pair for a processed transfer.
func DoubleEntry(transferID, fromWalletID, toWalletID string, amount int64) (debit, credit *LedgerEntry) {
	now := time.Now().UTC()
	debit = &LedgerEntry{
		TransferID: transferID,
		WalletID:   fromWalletID,
		EntryType:  EntryDebit,
		Amount:     amount,
		CreatedAt:  now,
	}
	credit = &LedgerEntry{
		TransferID: transferID,
		WalletID:   toWalletID,
		EntryType:  EntryCredit,
		Amount:     amount,
		CreatedAt:  now,
	}
	return debit, credit
}
