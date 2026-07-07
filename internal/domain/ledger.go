package domain

type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
}
