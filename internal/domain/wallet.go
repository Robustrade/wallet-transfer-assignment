package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Wallet is a holder of funds identified by a UUID v7.
type Wallet struct {
	ID        string
	Balance   decimal.Decimal
	CreatedAt time.Time
	UpdatedAt time.Time
}
