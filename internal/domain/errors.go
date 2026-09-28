// Package domain holds the core business model: wallets, transfers, ledger
// entries and their rules. It has no knowledge of HTTP or databases.
package domain

import "errors"

// Sentinel errors. Callers match them with errors.Is; lower layers may wrap
// them with extra context using fmt.Errorf("...: %w", err).
var (
	ErrValidation           = errors.New("validation error")
	ErrWalletNotFound       = errors.New("wallet not found")
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrCurrencyMismatch     = errors.New("currency mismatch")
	ErrBalanceOverflow      = errors.New("balance overflow")
	ErrInvalidTransition    = errors.New("invalid status transition")
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with a different request")
)

// FailureReason explains why a transfer ended in FAILED. It is stored with
// the transfer and returned to clients, so the values are part of the API.
type FailureReason string

const (
	ReasonInsufficientFunds FailureReason = "INSUFFICIENT_FUNDS"
	ReasonCurrencyMismatch  FailureReason = "CURRENCY_MISMATCH"
	ReasonBalanceOverflow   FailureReason = "BALANCE_OVERFLOW"
)

// valid reports whether r is one of the known failure reasons.
func (r FailureReason) valid() bool {
	switch r {
	case ReasonInsufficientFunds, ReasonCurrencyMismatch, ReasonBalanceOverflow:
		return true
	}
	return false
}

// FailureReasonFor maps a business-rule error to the reason stored on a
// FAILED transfer. ok is false for every other error: those are not business
// outcomes (e.g. a missing wallet or a DB failure) and must roll back instead
// of being recorded as FAILED.
func FailureReasonFor(err error) (reason FailureReason, ok bool) {
	switch {
	case errors.Is(err, ErrInsufficientFunds):
		return ReasonInsufficientFunds, true
	case errors.Is(err, ErrCurrencyMismatch):
		return ReasonCurrencyMismatch, true
	case errors.Is(err, ErrBalanceOverflow):
		return ReasonBalanceOverflow, true
	}
	return "", false
}
