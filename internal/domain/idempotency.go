package domain

import "time"

type IdempotencyStatus string

const (
	IdempotencyInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyCompleted  IdempotencyStatus = "COMPLETED"
)

type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	Status         IdempotencyStatus
	TransferID     string
	ResponseStatus int
	ResponseBody   []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
