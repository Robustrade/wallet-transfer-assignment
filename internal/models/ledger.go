package models

type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

type Ledger struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	WalletID   string    `gorm:"not null;index" json:"wallet_id"`
	TransferID string    `gorm:"not null;index" json:"transfer_id"`
	Amount     int64     `gorm:"not null" json:"amount"` // Absolute value
	Type       EntryType `gorm:"not null" json:"type"`
	CreatedAt  int64     `gorm:"autoCreateTime" json:"created_at"`
}
