package domain

import "time"

// IdempotencyStatus tracks the lifecycle of an idempotency record.
type IdempotencyStatus string

const (
	// IdempotencyPending marks a key that has been claimed but whose transfer
	// has not yet reached a terminal state within the same transaction.
	IdempotencyPending IdempotencyStatus = "PENDING"
	// IdempotencyCompleted marks a key whose result is final and replayable.
	IdempotencyCompleted IdempotencyStatus = "COMPLETED"
)

// IdempotencyRecord is the durable dedup token for a request. The Key carries a
// UNIQUE constraint in the database; concurrent duplicate inserts block on that
// constraint until the first transaction commits, at which point the duplicate
// observes the conflict and replays ResponseBody instead of re-executing.
//
// RequestHash captures the semantically significant request fields so that
// reusing a key with a different payload can be detected and rejected.
type IdempotencyRecord struct {
	Key          string
	RequestHash  string
	TransferID   string
	ResponseBody []byte
	Status       IdempotencyStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
