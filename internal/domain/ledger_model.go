package domain

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
}
