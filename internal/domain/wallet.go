package domain

import (
	"time"
)

const (
	WalletStatusActive = "ACTIVE"
	WalletStatusFrozen = "FROZEN"
)

// Wallet represents an individual account balance entity.
type Wallet struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	Balance        int64     `gorm:"not null;default:0;check:balance >= 0" json:"balance"` // In minor currency units (e.g. cents)
	OpeningBalance int64     `gorm:"not null;default:0;check:opening_balance >= 0" json:"openingBalance"`
	Currency       string    `gorm:"size:3;not null;default:'USD'" json:"currency"`
	Status         string    `gorm:"size:20;not null;default:'ACTIVE'" json:"status"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

// TableName overrides the default table name in GORM.
func (Wallet) TableName() string {
	return "wallets"
}

// IsActive returns whether the wallet is permitted to transact.
func (w *Wallet) IsActive() bool {
	return w.Status == WalletStatusActive
}
