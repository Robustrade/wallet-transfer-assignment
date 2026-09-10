package domain

import "time"

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one side of a double-entry posting. A processed
// transfer always produces exactly one debit and one credit of equal
// amount. Failed transfers produce none.
type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

func NewPosting(transferID, fromWalletID, toWalletID, debitID, creditID string, amount int64, now time.Time) (debit LedgerEntry, credit LedgerEntry, err error) {
	if amount <= 0 {
		return LedgerEntry{}, LedgerEntry{}, ErrInvalidAmount
	}
	ts := now.UTC()
	debit = LedgerEntry{
		ID:         debitID,
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       EntryDebit,
		Amount:     amount,
		CreatedAt:  ts,
	}
	credit = LedgerEntry{
		ID:         creditID,
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       EntryCredit,
		Amount:     amount,
		CreatedAt:  ts,
	}
	return debit, credit, nil
}

func (d LedgerEntry) Balances(c LedgerEntry) bool {
	return d.Type == EntryDebit &&
		c.Type == EntryCredit &&
		d.Amount == c.Amount &&
		d.Amount > 0 &&
		d.TransferID == c.TransferID &&
		d.WalletID != c.WalletID
}
