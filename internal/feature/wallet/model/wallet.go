package model

import "time"

type Wallet struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Balance   int64     `json:"balance" gorm:"not null;default:0"`
	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (Wallet) TableName() string {
	return "wallets"
}
