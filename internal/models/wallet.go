package models

type Wallet struct {
	ID        string `gorm:"primaryKey;type:uuid" json:"id"`
	Balance   int64  `gorm:"not null;check:balance >= 0" json:"balance"` // Amount in cents/paise
	CreatedAt int64  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updated_at"`
}
