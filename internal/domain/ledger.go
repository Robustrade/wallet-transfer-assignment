package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// LedgerType is the direction of a ledger entry. Amounts are always stored as
// positive numbers; direction is encoded here.
type LedgerType string

const (
	LedgerDebit  LedgerType = "DEBIT"
	LedgerCredit LedgerType = "CREDIT"
)

// Valid reports whether t is a known ledger type.
func (t LedgerType) Valid() bool {
	return t == LedgerDebit || t == LedgerCredit
}

// LedgerEntry is one half of a double-entry pair. Every processed transfer
// produces exactly two entries: a DEBIT on the source wallet and a CREDIT on
// the destination wallet, with equal positive amounts.
type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       LedgerType
	Amount     decimal.Decimal
	CreatedAt  time.Time
}
