package domain

import (
	"time"
)

type Wallet struct {
	ID        string    `json:"id" gorm:"primaryKey;type:varchar(255)"`
	Balance   int64     `json:"balance" gorm:"not null;default:0;check:chk_balance_non_negative,balance >= 0"`
	CreatedAt time.Time `json:"createdAt" gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"not null;default:CURRENT_TIMESTAMP"`
}
