package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type IdempotencyStatus string

const (
	IdempotencyStatusPending   IdempotencyStatus = "PENDING"
	IdempotencyStatusCompleted IdempotencyStatus = "COMPLETED"
)

// IdempotencyRecord persists API request execution results to achieve exactly-once semantics.
type IdempotencyRecord struct {
	Key            string            `json:"key"`
	RequestHash    string            `json:"requestHash"`
	Status         IdempotencyStatus `json:"status"`
	ResponseStatus int               `json:"responseStatus"`
	ResponseBody   string            `json:"responseBody"`
	TransferID     *string           `json:"transferId,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// ComputePayloadHash returns the deterministic SHA-256 hash of transfer payload arguments.
func ComputePayloadHash(fromWalletID, toWalletID string, amount int64) string {
	payload := fmt.Sprintf("%s|%s|%d", fromWalletID, toWalletID, amount)
	hasher := sha256.New()
	hasher.Write([]byte(payload))
	return hex.EncodeToString(hasher.Sum(nil))
}
