package models

import (
	"time"

	"github.com/google/uuid"
)

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry is one leg of a double-entry bookkeeping record.
// Every processed transfer produces exactly two of these: one DEBIT on the
// source wallet, one CREDIT on the destination wallet, both for the same amount.
type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

// NewLedgerPair builds the two ledger entries for a processed transfer.
// Keeping this as a single constructor guarantees the pair is always created
// together and always balances (same transferID, same amount, opposite legs).
func NewLedgerPair(transferID, fromWalletID, toWalletID uuid.UUID, amount int64) (debit, credit LedgerEntry) {
	debit = LedgerEntry{
		ID:         uuid.New(),
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       LedgerDebit,
		Amount:     amount,
	}
	credit = LedgerEntry{
		ID:         uuid.New(),
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       LedgerCredit,
		Amount:     amount,
	}
	return debit, credit
}
