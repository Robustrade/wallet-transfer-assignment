package domain

//go:generate stringer -type=EntryType -linecomment

// EntryType is the side of a ledger entry.
type EntryType int

const (
	Debit  EntryType = iota // DEBIT
	Credit                  // CREDIT
)

// LedgerEntry is one half of the double entry pair.
type LedgerEntry struct {
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
}
