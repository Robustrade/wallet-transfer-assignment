package domain

import (
	"time"

	"github.com/google/uuid"
)

type Wallet struct {
	ID        uuid.UUID
	Balance   Amount
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
}
