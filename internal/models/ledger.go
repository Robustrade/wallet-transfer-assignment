package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TransferID uuid.UUID       `gorm:"type:uuid;not null"`
	WalletID   uuid.UUID       `gorm:"type:uuid;not null"`
	Type       EntryType       `gorm:"not null"`
	Amount     decimal.Decimal `gorm:"type:numeric(16,2);not null"`
	CreatedAt  time.Time
}

func (LedgerEntry) TableName() string { return "ledger_entries" }
