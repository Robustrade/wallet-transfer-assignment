package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Status is the final, immutable outcome of a transfer. There is no
// persisted PENDING state — see docs/DESIGN.md §4.
type Status string

// The only two statuses a transfer row is ever written with.
const (
	StatusProcessed Status = "PROCESSED"
	StatusFailed    Status = "FAILED"
)

// Transfer is a single wallet-to-wallet transfer attempt. Its ID doubles
// as the idempotency key (see docs/DESIGN.md §5).
type Transfer struct {
	ID            int64
	RequestHash   string
	FromWalletID  string
	ToWalletID    string
	Amount        decimal.Decimal
	Status        Status
	FailureReason *ErrorCode
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
