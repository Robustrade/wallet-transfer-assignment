package model

import "time"

type Transfer struct {
	ID             int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	IdempotencyKey string    `json:"idempotencyKey" gorm:"uniqueIndex;size:64;not null"`
	FromWalletID   int64     `json:"fromWalletId" gorm:"not null"`
	ToWalletID     int64     `json:"toWalletId" gorm:"not null"`
	Amount         int64     `json:"amount" gorm:"not null"`
	Status         string    `json:"status" gorm:"size:20;not null;default:PENDING"`
	FailureReason  *string   `json:"failureReason,omitempty" gorm:"type:text"`
	CreatedAt      time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt      time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (Transfer) TableName() string {
	return "transfers"
}
