package domain

import (
	"time"
)

type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string    `json:"id" gorm:"primaryKey;type:varchar(255)"`
	WalletID   string    `json:"walletId" gorm:"not null;type:varchar(255);index;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;foreignKey:WalletID;references:wallets(id)"`
	TransferID string    `json:"transferId" gorm:"not null;type:varchar(255);index;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;foreignKey:TransferID;references:transfers(id)"`
	Type       EntryType `json:"type" gorm:"not null;type:varchar(50)"`
	Amount     int64     `json:"amount" gorm:"not null;check:chk_entry_amount_positive,amount > 0"`
	CreatedAt  time.Time `json:"createdAt" gorm:"not null;default:CURRENT_TIMESTAMP"`
}
