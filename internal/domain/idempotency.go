package domain

import (
	"time"
)

type IdempotencyStatus string

const (
	IdempotencyStatusPending IdempotencyStatus = "PENDING"
	IdempotencyStatusSuccess IdempotencyStatus = "SUCCESS"
	IdempotencyStatusFailed  IdempotencyStatus = "FAILED"
)

type IdempotencyRecord struct {
	IdempotencyKey string            `json:"idempotencyKey" gorm:"primaryKey;type:varchar(255)"`
	Status         IdempotencyStatus `json:"status" gorm:"not null;type:varchar(50)"`
	ResponseCode   int               `json:"responseCode" gorm:"not null"`
	ResponseBody   string            `json:"responseBody" gorm:"type:text"`
	CreatedAt      time.Time         `json:"createdAt" gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt      time.Time         `json:"updatedAt" gorm:"not null;default:CURRENT_TIMESTAMP"`
}
