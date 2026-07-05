package domain

import (
	"time"

	"github.com/google/uuid"
)

type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	TransferID     uuid.UUID
	CreatedAt      time.Time
}
