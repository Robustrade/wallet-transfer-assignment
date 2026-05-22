package cache

import "time"

const (
	CacheTTL = 24 * time.Hour
)

type IdempotencyState string

const (
	StateIssued    IdempotencyState = "ISSUED"
	StateInFlight  IdempotencyState = "IN_FLIGHT"
	StateCompleted IdempotencyState = "COMPLETED"
)
