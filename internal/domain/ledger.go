package domain

import "time"

type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string    `json:"id"`
	WalletID   string    `json:"walletId"`
	TransferID string    `json:"transferId"`
	Type       EntryType `json:"type"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}
