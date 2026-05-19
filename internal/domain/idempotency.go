package domain

import "time"

// IdempotencyRecord stores a prior API response for replay.
type IdempotencyRecord struct {
	Key            string
	TransferID     string
	RequestHash    string
	ResponseStatus int
	ResponseBody   string
	CreatedAt      time.Time
}
