package models

import (
	"time"

	"github.com/google/uuid"
)

type IdempotencyStatus string

const (
	IdempotencyInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyFailed     IdempotencyStatus = "FAILED"
)

type IdempotencyRecord struct {
	Key          string            `gorm:"column:idempotency_key;primaryKey"`
	RequestHash  string            `gorm:"not null"`
	TransferID   *uuid.UUID        `gorm:"type:uuid"`
	Status       IdempotencyStatus `gorm:"not null"`
	ResponseBody []byte            `gorm:"type:jsonb"`
	CreatedAt    time.Time         `gorm:"<-:false"`
	UpdatedAt    time.Time         `gorm:"<-:false"`
}

func (IdempotencyRecord) TableName() string { return "idempotency_records" }
