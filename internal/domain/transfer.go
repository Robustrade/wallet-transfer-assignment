package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Status is the final, immutable outcome of a transfer. A transfer's
// debit/credit/ledger sequence is one atomic DB transaction, so nothing
// mid-flight is ever written — there is no persisted PENDING state.
type Status string

// The only two statuses a transfer row is ever written with.
const (
	StatusProcessed Status = "PROCESSED"
	StatusFailed    Status = "FAILED"
)

// Transfer is a single wallet-to-wallet transfer attempt. Its ID is
// hand-rolled (Snowflake, time-ordered), not a DB serial, because it
// doubles as the PK, the idempotency key, and the ledger_entries FK
// target — one identifier, no translation layer.
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
