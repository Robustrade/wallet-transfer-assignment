package domain

import (
	"time"

	"github.com/google/uuid"
)

// NatureWalletID is the static UUID of the system "nature" wallet that funds
// magic deposits. It is created once by a database migration.
var NatureWalletID = uuid.MustParse("c0ffee00-0000-0000-0000-000000000001")

// Wallet holds a user's monetary balance.
type Wallet struct {
	ID        uuid.UUID
	Balance   Amount
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewWallet creates a Wallet with a zero balance and a freshly generated UUID v7.
func NewWallet() Wallet {
	now := time.Now().UTC()

	return Wallet{
		ID:        NewID(),
		Balance:   0,
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
