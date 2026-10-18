package domain

import (
	"errors"
	"time"
)

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

var (
	ErrInvalidLedgerEntry = errors.New("invalid ledger entry")
)

type LedgerEntry struct {
	ID         int64
	TransferID int64
	WalletID   int64
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

func (l LedgerEntry) Validate() error {
	if l.TransferID <= 0 || l.WalletID <= 0 {
		return ErrInvalidLedgerEntry
	}

	if l.Amount <= 0 {
		return ErrInvalidAmount
	}

	if l.EntryType != LedgerDebit && l.EntryType != LedgerCredit {
		return ErrInvalidLedgerEntry
	}

	return nil
}

// NewLedgerEntries creates the debit and credit pair for a transfer.
func NewLedgerEntries(transferID, fromWalletID, toWalletID, amount int64) ([]LedgerEntry, error) {
	if transferID <= 0 ||
		fromWalletID <= 0 ||
		toWalletID <= 0 ||
		fromWalletID == toWalletID {
		return nil, ErrInvalidLedgerEntry
	}

	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	entries := []LedgerEntry{
		{
			TransferID: transferID,
			WalletID:   fromWalletID,
			EntryType:  LedgerDebit,
			Amount:     amount,
		},
		{
			TransferID: transferID,
			WalletID:   toWalletID,
			EntryType:  LedgerCredit,
			Amount:     amount,
		},
	}

	return entries, nil
}
