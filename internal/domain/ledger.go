package domain

import "time"

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string          `json:"id"`
	TransferID string          `json:"transferId"`
	WalletID   string          `json:"walletId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"`
	CreatedAt  time.Time       `json:"createdAt"`
}
