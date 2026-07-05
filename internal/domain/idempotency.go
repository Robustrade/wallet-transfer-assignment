package domain

import "time"

type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
)

type IdempotencyRecord struct {
	IdempotencyKey     string
	RequestFingerprint string
	TransferID         *string
	Status             IdempotencyStatus
	ResponseSnapshot   []byte
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
