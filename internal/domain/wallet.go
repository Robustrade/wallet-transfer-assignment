package domain

import (
	"time"

	"github.com/google/uuid"
)

// Wallet is the domain entity for a wallet. Balance is always the current
// stored balance (never derived by summing the ledger on the read path —
// see docs/design.md for why).
type Wallet struct {
	ID        uuid.UUID
	Name      string
	Balance   int64 // minor units (cents)
	CreatedAt time.Time
	UpdatedAt time.Time
}
