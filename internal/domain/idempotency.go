package domain

import "time"

type IdempotencyRecord struct {
	Key        string
	Response   string // Serialized JSON response
	StatusCode int
	CreatedAt  time.Time
}
