package domain

import (
	"time"
)

// TransferStatus represents the current state in the transfer lifecycle.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// Transfer records the intent and outcome of a balance transfer between two wallets.
type Transfer struct {
	ID             string         `gorm:"primaryKey;size:64" json:"id"`
	IdempotencyKey string         `gorm:"size:255;not null;uniqueIndex" json:"idempotencyKey"`
	FromWalletID   string         `gorm:"size:64;not null;index" json:"fromWalletId"`
	ToWalletID     string         `gorm:"size:64;not null;index" json:"toWalletId"`
	Amount         int64          `gorm:"not null;check:amount > 0" json:"amount"` // In minor currency units
	Currency       string         `gorm:"size:3;not null;default:'USD'" json:"currency"`
	Status         TransferStatus `gorm:"size:20;not null" json:"status"`
	FailureReason  string         `gorm:"type:text" json:"failureReason,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updatedAt"`
}

// TableName overrides the default table name in GORM.
func (Transfer) TableName() string {
	return "transfers"
}
