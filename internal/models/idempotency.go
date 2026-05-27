package models

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// IdempotencyStatus represents the state of an idempotency record
type IdempotencyStatus string

const (
	IdempotencyStatusProcessing IdempotencyStatus = "PROCESSING"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

// IdempotencyRecord prevents duplicate request processing
type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	Status         IdempotencyStatus
	TransferID     *string    // Nullable until transfer is created
	ResponseData   []byte     // JSON-encoded response (stored as JSONB in DB)
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate validates idempotency record data
func (i *IdempotencyRecord) Validate() error {
	if i.IdempotencyKey == "" {
		return errors.New("idempotency key cannot be empty")
	}
	if i.RequestHash == "" {
		return errors.New("request hash cannot be empty")
	}
	if !i.IsValidStatus() {
		return errors.New("invalid idempotency status")
	}
	return nil
}

// IsValidStatus checks if the idempotency status is valid
func (i *IdempotencyRecord) IsValidStatus() bool {
	switch i.Status {
	case IdempotencyStatusProcessing, IdempotencyStatusCompleted, IdempotencyStatusFailed:
		return true
	default:
		return false
	}
}

// IsProcessing checks if request is still being processed
func (i *IdempotencyRecord) IsProcessing() bool {
	return i.Status == IdempotencyStatusProcessing
}

// IsCompleted checks if request completed successfully
func (i *IdempotencyRecord) IsCompleted() bool {
	return i.Status == IdempotencyStatusCompleted
}

// IsFailed checks if request failed
func (i *IdempotencyRecord) IsFailed() bool {
	return i.Status == IdempotencyStatusFailed
}

// MarkAsCompleted updates status to COMPLETED
func (i *IdempotencyRecord) MarkAsCompleted(transferID string, responseData []byte) {
	i.Status = IdempotencyStatusCompleted
	i.TransferID = &transferID
	i.ResponseData = responseData
	i.UpdatedAt = time.Now()
}

// MarkAsFailed updates status to FAILED
func (i *IdempotencyRecord) MarkAsFailed(responseData []byte) {
	i.Status = IdempotencyStatusFailed
	i.ResponseData = responseData
	i.UpdatedAt = time.Now()
}

// ValidateRequestHash checks if the provided hash matches the stored hash
func (i *IdempotencyRecord) ValidateRequestHash(requestHash string) error {
	if i.RequestHash != requestHash {
		return errors.New("idempotency key reused with different payload")
	}
	return nil
}

// GenerateRequestHash creates a SHA256 hash from request parameters
func GenerateRequestHash(fromWalletID, toWalletID string, amount int64) string {
	data := fmt.Sprintf("%s:%s:%d", fromWalletID, toWalletID, amount)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
