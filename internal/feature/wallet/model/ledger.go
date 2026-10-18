package model

import "time"

type LedgerEntry struct {
	ID         int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	TransferID int64     `json:"transferId" gorm:"not null;uniqueIndex:idx_transfer_entry_type"`
	WalletID   int64     `json:"walletId" gorm:"not null"`
	EntryType  string    `json:"entryType" gorm:"size:10;not null;uniqueIndex:idx_transfer_entry_type"`
	Amount     int64     `json:"amount" gorm:"not null"`
	CreatedAt  time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (LedgerEntry) TableName() string {
	return "ledger_entries"
}
