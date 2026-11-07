package domain

import "github.com/google/uuid"

// Wallet holds a balance in minor units. The balance can never go below zero.
type Wallet struct {
	ID      string
	Balance int64
}

// CanDebit reports whether the wallet can pay amount without going negative.
func (w Wallet) CanDebit(amount int64) bool {
	return false // TODO: implement
}

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one side of a double-entry record. Amount is always positive;
// the direction is carried by Type.
type LedgerEntry struct {
	TransferID uuid.UUID
	WalletID   string
	Type       EntryType
	Amount     int64
}
