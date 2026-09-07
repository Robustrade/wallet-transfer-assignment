package model

import "time"

type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID            int64           `json:"id"`
	WalletID      string          `json:"wallet_id"`
	TransferID    string          `json:"transfer_id"`
	Type          LedgerEntryType `json:"type"`
	Amount        int64           `json:"amount"`
	BalanceBefore int64           `json:"balance_before"`
	BalanceAfter  int64           `json:"balance_after"`
	CreatedAt     time.Time       `json:"created_at"`
}
