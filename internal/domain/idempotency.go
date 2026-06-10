package domain

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// IdempotencyStatus represents the processing state of an idempotent request.
type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

// IdempotencyRecord tracks a unique request to prevent duplicate processing.
// It acts as a gate before the transfers table is touched.
type IdempotencyRecord struct {
	IdempotencyKey string            `json:"idempotencyKey"`
	RequestHash    string            `json:"requestHash"`
	TransferID     *string           `json:"transferId,omitempty"`
	Status         IdempotencyStatus `json:"status"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// ComputeRequestHash computes a SHA-256 hash of the transfer request parameters.
// This is used to detect when the same idempotency key is reused with different parameters.
func ComputeRequestHash(fromWalletID, toWalletID string, amount int64) string {
	data := fmt.Sprintf("%s:%s:%d", fromWalletID, toWalletID, amount)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)
}
