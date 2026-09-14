// Package domain holds the entities, state transitions, and validation rules of
// the wallet transfer service. It knows nothing about HTTP or SQL: everything
// here is true regardless of how a transfer arrives or where it is stored.
package domain

import (
	"errors"
	"fmt"
)

// ErrValidation is the category every validation error reports itself as, so a
// caller can map the whole class to 400 with one check. Matching the category
// rather than listing each error means a rule added later cannot be forgotten
// by the handler and silently become a 500.
var ErrValidation = errors.New("invalid request")

// ValidationError describes a request that is malformed on its face,
// independent of any state in the database.
type ValidationError struct {
	// Field is the request field at fault, in the spelling the client sent.
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Is reports every ValidationError as ErrValidation.
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// Validation errors. Each is a distinct value, so callers can still match one
// exactly with errors.Is; each also matches ErrValidation.
var (
	ErrMissingIdempotencyKey = &ValidationError{
		Field: "idempotencyKey", Message: "idempotencyKey is required"}
	ErrIdempotencyKeyTooLong = &ValidationError{
		Field:   "idempotencyKey",
		Message: fmt.Sprintf("idempotencyKey must be at most %d bytes", MaxIdempotencyKeyLength)}
	ErrMissingFromWallet = &ValidationError{
		Field: "fromWalletId", Message: "fromWalletId is required"}
	ErrMissingToWallet = &ValidationError{
		Field: "toWalletId", Message: "toWalletId is required"}
	ErrSameWallet = &ValidationError{
		Field: "toWalletId", Message: "fromWalletId and toWalletId must differ"}
	ErrNonPositiveAmount = &ValidationError{
		Field: "amount", Message: "amount must be greater than zero"}
)

// Outcome errors. These depend on stored state, and each maps to its own status
// code: see docs/design.md.
var (
	// ErrInsufficientFunds is recorded on the transfer and committed, so a
	// replay of the same idempotency key reports the same failure.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrWalletNotFound reports a wallet that does not exist. Match it with
	// errors.Is; use WalletNotFoundError to learn which wallet.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrIdempotencyKeyConflict reports a key replayed with different
	// parameters than it was first used with.
	ErrIdempotencyKeyConflict = errors.New("idempotency key already used with different parameters")

	// ErrWalletBusy reports that a wallet's row was locked by another transfer
	// for longer than the service is willing to wait. The request never
	// started, so retrying it unchanged is safe.
	ErrWalletBusy = errors.New("wallet is busy; retry the request")

	// ErrInvalidStateTransition reports an attempt to move a transfer out of a
	// state it is not in. It should be unreachable: every transition is guarded
	// in SQL as well, and it exists so a logic bug surfaces as an error rather
	// than as a silently skipped update.
	ErrInvalidStateTransition = errors.New("invalid state transition")
)

// WalletNotFoundError names the wallet that was missing, so the handler can say
// which one rather than making the caller guess.
type WalletNotFoundError struct {
	WalletID string
}

func (e *WalletNotFoundError) Error() string {
	return fmt.Sprintf("wallet %s does not exist", e.WalletID)
}

// Is reports WalletNotFoundError as ErrWalletNotFound, so callers can match the
// category with errors.Is and still recover the wallet id with errors.As.
func (e *WalletNotFoundError) Is(target error) bool {
	return target == ErrWalletNotFound
}
