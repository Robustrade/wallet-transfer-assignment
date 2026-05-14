package models

import (
	"time"

	"gorm.io/datatypes"
)

// IdempotencyRecord stores idempotency information for a transfer
type IdempotencyRecord struct {
	IdempotencyKey string         `gorm:"primaryKey;column:idempotency_key;index:idx_idempotency_key" json:"idempotency_key"`
	TransferID     string         `gorm:"column:transfer_id" json:"transfer_id"`
	Response       datatypes.JSON `gorm:"column:response;type:jsonb" json:"response"`
	CreatedAt      time.Time      `gorm:"column:created_at" json:"created_at"`
}

func (IdempotencyRecord) TableName() string {
	return "idempotency_records"
}
