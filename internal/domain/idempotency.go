package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// IdempotencyStatus tracks the execution state of an idempotent key.
type IdempotencyStatus string

const (
	IdempotencyStatusPending   IdempotencyStatus = "PENDING"
	IdempotencyStatusCompleted IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed    IdempotencyStatus = "FAILED"
)

// IdempotencyRecord stores request hashes and cached response bodies.
type IdempotencyRecord struct {
	Key          string            `gorm:"primaryKey;size:255" json:"key"`
	RequestHash  string            `gorm:"size:64;not null" json:"requestHash"`
	Status       IdempotencyStatus `gorm:"size:20;not null" json:"status"`
	ResponseCode int               `json:"responseCode"`
	ResponseBody string            `gorm:"type:text" json:"responseBody"`
	CreatedAt    time.Time         `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time         `gorm:"autoUpdateTime" json:"updatedAt"`
}

// TableName overrides the default table name in GORM.
func (IdempotencyRecord) TableName() string {
	return "idempotency_records"
}

// HashPayload generates a SHA-256 hash hex string of the request bytes.
func HashPayload(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}
