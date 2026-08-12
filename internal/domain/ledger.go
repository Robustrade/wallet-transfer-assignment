package domain

import (
	"time"

	"github.com/google/uuid"
)

// LedgerEntryType is DEBIT or CREDIT. A transfer produces exactly one of
// each, enforced by the database's UNIQUE (transfer_id, entry_type)
// constraint.
type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

// LedgerEntry is the domain representation of an append-only ledger row.
type LedgerEntry struct {
	ID           int64
	TransferID   uuid.UUID
	WalletID     string
	EntryType    LedgerEntryType
	AmountMinor  int64
	Currency     string
	BalanceAfter int64
	CreatedAt    time.Time
}

// NewDoubleEntry builds the balanced DEBIT/CREDIT pair for a processed
// transfer. fromBalanceAfter and toBalanceAfter are the post-update balances
// returned by the guarded UPDATE statements, so the ledger and the stored
// balance are written from the same values inside the same transaction.
func NewDoubleEntry(transferID uuid.UUID, fromWalletID, toWalletID string, amountMinor int64, currency string, fromBalanceAfter, toBalanceAfter int64) (debit, credit LedgerEntry) {
	debit = LedgerEntry{
		TransferID:   transferID,
		WalletID:     fromWalletID,
		EntryType:    LedgerEntryDebit,
		AmountMinor:  amountMinor,
		Currency:     currency,
		BalanceAfter: fromBalanceAfter,
	}
	credit = LedgerEntry{
		TransferID:   transferID,
		WalletID:     toWalletID,
		EntryType:    LedgerEntryCredit,
		AmountMinor:  amountMinor,
		Currency:     currency,
		BalanceAfter: toBalanceAfter,
	}
	return debit, credit
}
