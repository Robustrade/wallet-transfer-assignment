package domain

import "github.com/google/uuid"

type Wallet struct {
	ID        uuid.UUID
	Balance   int64
	CreatedAt string
}
