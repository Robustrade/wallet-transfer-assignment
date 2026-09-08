package domain

import "fmt"

// EntryType is the direction of a ledger entry.
type EntryType string

const (
	// EntryDebit reduces a wallet's position.
	EntryDebit EntryType = "DEBIT"
	// EntryCredit increases a wallet's position.
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one half of a double-entry record. Every transfer produces
// exactly one DEBIT and one CREDIT of equal amount.
type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID string
	Type       EntryType
	Amount     Money
}

// NewLedgerPair builds the debit/credit pair for a transfer. The two entries
// always carry the same amount so the ledger balances to zero per transfer.
func NewLedgerPair(transferID, fromWalletID, toWalletID string, amount Money) (debit, credit LedgerEntry, err error) {
	if !amount.IsPositive() {
		return LedgerEntry{}, LedgerEntry{}, fmt.Errorf("%w: ledger amount must be positive", ErrValidation)
	}
	debit = LedgerEntry{
		WalletID:   fromWalletID,
		TransferID: transferID,
		Type:       EntryDebit,
		Amount:     amount,
	}
	credit = LedgerEntry{
		WalletID:   toWalletID,
		TransferID: transferID,
		Type:       EntryCredit,
		Amount:     amount,
	}
	return debit, credit, nil
}
