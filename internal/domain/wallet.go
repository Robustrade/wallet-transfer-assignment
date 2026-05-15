package domain

import (
	"github.com/google/uuid"
	"time"
)

type Wallet struct {
	ID        uuid.UUID
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
