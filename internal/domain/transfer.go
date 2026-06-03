package domain

import (
	"time"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID           string         `json:"transferId" gorm:"primaryKey;type:varchar(255)"`
	FromWalletID string         `json:"fromWalletId" gorm:"not null;type:varchar(255);index;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;foreignKey:FromWalletID;references:wallets(id)"`
	ToWalletID   string         `json:"toWalletId" gorm:"not null;type:varchar(255);index;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;foreignKey:ToWalletID;references:wallets(id)"`
	Amount       int64          `json:"amount" gorm:"not null;check:chk_amount_positive,amount > 0"`
	Status       TransferStatus `json:"status" gorm:"not null;type:varchar(50)"`
	CreatedAt    time.Time      `json:"createdAt" gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time      `json:"updatedAt" gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (t *Transfer) Validate() error {
	if t.FromWalletID == "" || t.ToWalletID == "" {
		return ErrWalletNotFound
	}
	if t.FromWalletID == t.ToWalletID {
		return ErrSameWallet
	}
	if t.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
