package domain

import (
	"time"

	"github.com/google/uuid"
)

// IdempotencyStatus tracks the lifecycle of a single idempotency key.
type IdempotencyStatus string

const (
	IdempotencyInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyFailed     IdempotencyStatus = "FAILED"
)

// IdempotencyRecord binds a client-supplied idempotency key to exactly one
// logical request (identified by RequestHash) and stores the final HTTP
// response so retries can replay it verbatim instead of re-executing.
type IdempotencyRecord struct {
	ID             uuid.UUID
	IdempotencyKey string
	RequestHash    string
	TransferID     *uuid.UUID
	Status         IdempotencyStatus
	ResponseStatus *int
	ResponseBody   []byte // raw JSON
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
