package domain

type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
}
