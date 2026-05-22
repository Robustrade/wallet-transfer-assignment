package domain

// EntryType is the side of a double-entry ledger record.
type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

// LedgerEntry is one side of a transfer. Every transfer produces exactly two
// entries: a DEBIT on the source wallet and a CREDIT on the destination. The
// database enforces this with UNIQUE (transfer_id, type).
type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
}
