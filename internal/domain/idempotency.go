package domain

import "time"

type IdempotencyStatus string

const (
	IdempotencyPending   IdempotencyStatus = "PENDING"
	IdempotencyCompleted IdempotencyStatus = "COMPLETED"
)

// IdempotencyRecord tracks one idempotency key end to end: the request
// fingerprint that was first seen for it, and — once processing finishes —
// which transfer to replay for any future duplicate.
type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	Status         IdempotencyStatus
	TransferID     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
