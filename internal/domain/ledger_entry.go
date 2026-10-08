package domain

import (
	"fmt"
	"time"
)

type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

func (kind LedgerEntryType) Validate() error {
	switch kind {
	case LedgerEntryDebit, LedgerEntryCredit:
		return nil
	default:
		return fmt.Errorf("invalid ledger entry type: %q", kind)
	}
}

// LedgerEntry records a positive minor-unit amount; Type determines direction.
type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

func (entry LedgerEntry) Validate() error {
	if entry.Amount <= 0 {
		return fmt.Errorf("ledger entry amount must be positive")
	}
	return entry.Type.Validate()
}
