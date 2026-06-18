package domain

type LedgerType string

const (
	Debit  LedgerType = "DEBIT"
	Credit LedgerType = "CREDIT"
)

type LedgerEntry struct {
	ID string

	TransferID string
	WalletID   string

	Type LedgerType

	Amount int64
}