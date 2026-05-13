package models

import "time"

// Wallet represents a wallet
type Wallet struct {
	WalletID  string    `gorm:"primaryKey;column:wallet_id" json:"wallet_id"`
	Balance   int64     `gorm:"column:balance" json:"balance"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (Wallet) TableName() string {
	return "wallets"
}
