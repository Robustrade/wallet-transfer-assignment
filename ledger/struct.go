package ledger

import "time"

type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uint64    `json:"id"`
	WalletID   string    `json:"walletId"`
	TransferID uint64    `json:"transferId"`
	Type       EntryType `json:"type"`
	Amount     float64   `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}
