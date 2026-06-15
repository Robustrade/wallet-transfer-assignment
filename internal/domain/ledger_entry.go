package domain

import (
	"time"

	"github.com/google/uuid"
)

// EntryType distinguishes debit (money out) from credit (money in).
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is a single line in the double-entry ledger.
// Every completed transfer produces exactly one DEBIT and one CREDIT entry.
type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

// NewLedgerPair constructs the two entries required for a transfer.
// The caller is responsible for persisting both atomically.
func NewLedgerPair(transferID uuid.UUID, fromWalletID, toWalletID string, amount int64) (debit, credit LedgerEntry) {
	debit = LedgerEntry{
		ID:         uuid.New(),
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       EntryDebit,
		Amount:     amount,
	}
	credit = LedgerEntry{
		ID:         uuid.New(),
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       EntryCredit,
		Amount:     amount,
	}
	return debit, credit
}
