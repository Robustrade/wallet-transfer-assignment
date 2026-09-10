package domain

import "time"

// EntryType represents the type of ledger entry.
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// LedgerEntry represents a single entry in the double-entry ledger.
type LedgerEntry struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	EntryType  EntryType `json:"entryType"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// NewDebitEntry creates a debit ledger entry for the source wallet.
func NewDebitEntry(transferID, walletID string, amount int64) *LedgerEntry {
	return &LedgerEntry{
		TransferID: transferID,
		WalletID:   walletID,
		EntryType:  EntryTypeDebit,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}
}

// NewCreditEntry creates a credit ledger entry for the destination wallet.
func NewCreditEntry(transferID, walletID string, amount int64) *LedgerEntry {
	return &LedgerEntry{
		TransferID: transferID,
		WalletID:   walletID,
		EntryType:  EntryTypeCredit,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}
}
