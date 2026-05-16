package domain

import (
	"time"
)

// LedgerEntryType represents the type of ledger entry
type LedgerEntryType string

const (
	TypeDebit  LedgerEntryType = "DEBIT"
	TypeCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry represents a double-entry ledger record
type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       LedgerEntryType
	Amount     int64 // Amount in cents
	CreatedAt  time.Time
}

// NewLedgerEntry creates a new ledger entry
func NewLedgerEntry(id string, transferID string, walletID string, entryType LedgerEntryType, amount int64) *LedgerEntry {
	return &LedgerEntry{
		ID:         id,
		TransferID: transferID,
		WalletID:   walletID,
		Type:       entryType,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}
}
