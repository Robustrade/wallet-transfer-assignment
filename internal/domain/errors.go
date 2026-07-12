// Package domain holds the pure business types and rules for wallets and transfers.
package domain

// ErrorCode identifies a specific domain failure, used both in API
// responses and, where persisted, in transfers.failure_reason.
type ErrorCode string

// The full set of typed domain errors. Each maps to one HTTP status and,
// where persisted, one transfers.failure_reason value.
const (
	ErrCodeInvalidAmount         ErrorCode = "INVALID_AMOUNT"
	ErrCodeSameWallet            ErrorCode = "SAME_WALLET"
	ErrCodeWalletNotFound        ErrorCode = "WALLET_NOT_FOUND"
	ErrCodeInsufficientBalance   ErrorCode = "INSUFFICIENT_BALANCE"
	ErrCodeIdempotencyKeyReused  ErrorCode = "IDEMPOTENCY_KEY_REUSED"
	ErrCodeTransferInitFailed    ErrorCode = "TRANSFER_INIT_FAILED"
	ErrCodeInvalidAmountFormat   ErrorCode = "INVALID_AMOUNT_FORMAT"
	ErrCodeInvalidWalletID       ErrorCode = "INVALID_WALLET_ID"
	ErrCodeInvalidIdempotencyKey ErrorCode = "INVALID_IDEMPOTENCY_KEY"
)

// Error is a typed domain failure, distinguishable from unexpected
// (500-mapped) errors via errors.As.
type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Message }

// ErrInvalidAmountFormat is a tier-1 structural failure: the amount
// either doesn't parse as a decimal at all, or parses but overflows
// int64 once shifted into minor units. Kept distinct from
// ErrCodeInvalidAmount (a tier-2 business check for amount <= 0) so the
// two can carry different HTTP statuses — this one maps to 400,
// INVALID_AMOUNT maps to 422. Both share identical message text,
// distinguished only by Code and HTTP status.
var ErrInvalidAmountFormat = &Error{
	Code:    ErrCodeInvalidAmountFormat,
	Message: "Invalid transfer amount",
}

// ErrTransferInitFailed is returned when minting a new transfer ID
// fails (e.g. a Snowflake clock regression) — an infrastructure problem,
// not a client mistake, so the message stays generic and actionable
// rather than exposing why. The real cause is logged server-side at the
// call site, never returned to the client.
var ErrTransferInitFailed = &Error{
	Code:    ErrCodeTransferInitFailed,
	Message: "unable to initiate transfer, please try again after some time",
}
