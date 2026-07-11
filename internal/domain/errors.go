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
	ErrCodeAmountUnrepresentable ErrorCode = "AMOUNT_UNREPRESENTABLE"
	ErrCodeTransferInitFailed    ErrorCode = "TRANSFER_INIT_FAILED"
)

// Error is a typed domain failure, distinguishable from unexpected
// (500-mapped) errors via errors.As.
type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Message }

// ErrAmountUnrepresentable is a tier-1 structural failure: the client's
// amount, once shifted into minor units, doesn't fit in an int64. Kept
// distinct from ErrCodeInvalidAmount (a tier-2 business check for
// amount <= 0) so the two can carry different HTTP statuses — this one
// maps to 400, INVALID_AMOUNT maps to 422.
var ErrAmountUnrepresentable = &Error{
	Code:    ErrCodeAmountUnrepresentable,
	Message: "amount is too large or too precise to represent",
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
