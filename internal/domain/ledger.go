package domain

import (
	"time"
)

// LedgerEntryType represents whether money was debited from or credited to a wallet.
type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry represents an immutable record in the double-entry accounting ledger.
type LedgerEntry struct {
	ID         string          `json:"id"`
	TransferID string          `json:"transferId"`
	WalletID   string          `json:"walletId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"` // Positive integer in minor units
	CreatedAt  time.Time       `json:"createdAt"`
}

// NewDoubleEntryPair constructs the balanced DEBIT and CREDIT pair for a given transfer.
func NewDoubleEntryPair(transferID, fromWalletID, toWalletID string, amount int64) (debit, credit LedgerEntry, err error) {
	if amount <= 0 {
		return LedgerEntry{}, LedgerEntry{}, ErrInvalidAmount
	}
	if fromWalletID == toWalletID {
		return LedgerEntry{}, LedgerEntry{}, ErrSameWalletTransfer
	}

	now := time.Now().UTC()
	debit = LedgerEntry{
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       LedgerEntryTypeDebit,
		Amount:     amount,
		CreatedAt:  now,
	}

	credit = LedgerEntry{
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       LedgerEntryTypeCredit,
		Amount:     amount,
		CreatedAt:  now,
	}

	return debit, credit, nil
}
