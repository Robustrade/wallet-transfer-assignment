// Package domain holds the pure business types and rules for wallets and
// transfers. It has zero imports of database/sql, gin, or any infra
// package — see docs/DESIGN.md §8.
package domain

// ErrorCode identifies a specific domain failure, used both in API
// responses and, where persisted, in transfers.failure_reason.
type ErrorCode string

// The full set of typed domain errors — see docs/DESIGN.md §11.
const (
	ErrCodeInvalidAmount       ErrorCode = "INVALID_AMOUNT"
	ErrCodeSameWallet          ErrorCode = "SAME_WALLET"
	ErrCodeWalletNotFound      ErrorCode = "WALLET_NOT_FOUND"
	ErrCodeInsufficientBalance ErrorCode = "INSUFFICIENT_BALANCE"
)

// Error is a typed domain failure, distinguishable from unexpected
// (500-mapped) errors via errors.As.
type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Message }
