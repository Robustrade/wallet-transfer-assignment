package models

import (
	"errors"
	"time"
)

// EntryType represents the type of ledger entry
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// LedgerEntry represents a double-entry ledger record
// IMPORTANT: Ledger entries are IMMUTABLE (append-only)
type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	EntryType  EntryType
	Amount     int64 // Amount in cents (minor units)
	CreatedAt  time.Time
}

// Validate validates ledger entry data
func (l *LedgerEntry) Validate() error {
	if l.ID == "" {
		return errors.New("ledger entry ID cannot be empty")
	}
	if l.WalletID == "" {
		return errors.New("wallet ID cannot be empty")
	}
	if l.TransferID == "" {
		return errors.New("transfer ID cannot be empty")
	}
	if l.Amount <= 0 {
		return errors.New("ledger entry amount must be positive")
	}
	if !l.IsValidEntryType() {
		return errors.New("invalid entry type")
	}
	return nil
}

// IsValidEntryType checks if the entry type is valid
func (l *LedgerEntry) IsValidEntryType() bool {
	return l.EntryType == EntryTypeDebit || l.EntryType == EntryTypeCredit
}

// IsDebit checks if this is a debit entry
func (l *LedgerEntry) IsDebit() bool {
	return l.EntryType == EntryTypeDebit
}

// IsCredit checks if this is a credit entry
func (l *LedgerEntry) IsCredit() bool {
	return l.EntryType == EntryTypeCredit
}

// NewDebitEntry creates a new debit ledger entry
func NewDebitEntry(id, walletID, transferID string, amount int64) *LedgerEntry {
	return &LedgerEntry{
		ID:         id,
		WalletID:   walletID,
		TransferID: transferID,
		EntryType:  EntryTypeDebit,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}
}

// NewCreditEntry creates a new credit ledger entry
func NewCreditEntry(id, walletID, transferID string, amount int64) *LedgerEntry {
	return &LedgerEntry{
		ID:         id,
		WalletID:   walletID,
		TransferID: transferID,
		EntryType:  EntryTypeCredit,
		Amount:     amount,
		CreatedAt:  time.Now(),
	}
}
