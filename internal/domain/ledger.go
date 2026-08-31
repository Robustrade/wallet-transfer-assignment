package domain

type LedgerType string

const (
	LedgerDebit  LedgerType = "DEBIT"
	LedgerCredit LedgerType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID string
	Type       LedgerType
	Amount     int64
}
