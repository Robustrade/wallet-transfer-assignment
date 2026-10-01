package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Wallet struct {
	ID        uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	OwnerRef  string          `gorm:"not null"`
	Balance   decimal.Decimal `gorm:"type:numeric(16,2);not null"`
	CreatedAt time.Time
}

func (Wallet) TableName() string { return "wallets" }
