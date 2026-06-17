package idempotency

import "time"

type Status string

const (
	InProgress Status = "IN_PROGRESS"
	Completed  Status = "COMPLETED"
	Failed     Status = "FAILED"
)

type IdempotencyRecord struct {
	ID             uint64    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	RequestHash    string    `json:"requestHash"`
	TransferID     uint64    `json:"transferId,omitempty"`
	Response       []byte    `json:"response,omitempty"`
	Status         Status    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
