package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// IdempotencyStatus tracks the execution phase of an idempotent operation.
type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

// IdempotencyRecord stores request fingerprints and cached responses for duplicate request deduplication.
type IdempotencyRecord struct {
	IdempotencyKey string            `json:"idempotencyKey"`
	RequestHash    string            `json:"requestHash"`
	OwnerToken     string            `json:"ownerToken,omitempty"`
	TransferID     *string           `json:"transferId,omitempty"`
	Status         IdempotencyStatus `json:"status"`
	ResponseCode   int               `json:"responseCode"`
	ResponseBody   string            `json:"responseBody"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// ComputeRequestHash creates a deterministic SHA-256 digest of transfer parameters.
// Uses length-delimited fields to prevent delimiter-injection collisions (e.g. ("a", "b:c") vs ("a:b", "c")).
func ComputeRequestHash(fromWalletID, toWalletID string, amount int64) string {
	payload := fmt.Sprintf("%d:%s:%d:%s:%d", len(fromWalletID), fromWalletID, len(toWalletID), toWalletID, amount)
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}
