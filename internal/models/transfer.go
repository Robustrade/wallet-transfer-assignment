package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID           uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FromWalletID uuid.UUID       `gorm:"type:uuid;not null"`
	ToWalletID   uuid.UUID       `gorm:"type:uuid;not null"`
	Amount       decimal.Decimal `gorm:"type:numeric(16,2);not null"`
	Status       TransferStatus  `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (Transfer) TableName() string { return "transfers" }

// CanTransitionTo enforces PENDING -> PROCESSED | FAILED, nothing else.
func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	return s == TransferPending && (next == TransferProcessed || next == TransferFailed)
}
