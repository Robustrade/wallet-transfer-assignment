package model

import (
	"time"

	"gorm.io/datatypes"
)

type IdempotencyRecord struct {
	ID             int64          `json:"id" gorm:"primaryKey;autoIncrement"`
	IdempotencyKey string         `json:"idempotencyKey" gorm:"size:64;not null;uniqueIndex"`
	RequestHash    string         `json:"requestHash" gorm:"size:64;not null"`
	TransferID     *int64         `json:"transferId,omitempty" gorm:"column:transfer_id"`
	ResponseStatus *int           `json:"responseStatus,omitempty" gorm:"column:response_status"`
	ResponseBody   datatypes.JSON `json:"responseBody,omitempty" gorm:"type:jsonb"`
	CreatedAt      time.Time      `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt      time.Time      `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (IdempotencyRecord) TableName() string {
	return "idempotency_records"
}
