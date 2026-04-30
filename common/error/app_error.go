// Package apperr defines the typed application error model used across the service.
//
// AppError carries an HTTP status code and a stable error code so that handlers
// can map a domain-level error to a JSON response without each layer re-encoding
// HTTP semantics. Sentinel errors are declared in the per-domain files in this
// package.
package apperr

import (
	"errors"
	"fmt"
)

// AppError is the typed error returned across service boundaries.
//
// HTTPStatus is the response status. Code is a stable string code clients can
// switch on. Message is the human-readable summary. Wrapped, when set, allows
// callers to attach an underlying cause that errors.Is / errors.Unwrap can
// traverse.
type AppError struct {
	Code       string
	HTTPStatus int
	Message    string
	Wrapped    error
}

// Error implements error.
func (e *AppError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *AppError) Unwrap() error { return e.Wrapped }

// Is reports whether the target error is an *AppError with the same Code.
// Without this, errors.Is(err, ErrSentinel) would silently return false for
// errors produced by WithWrap / WithMessage — those return a *copy* of the
// sentinel, so the default == identity check on pointer values misses.
// Pinning equality to the Code field matches the user-facing contract: codes
// are the stable identifier, messages and wrapped causes are not.
func (e *AppError) Is(target error) bool {
	var ae *AppError
	if !errors.As(target, &ae) {
		return false
	}
	return e.Code == ae.Code
}

// WithWrap returns a copy of the error with the supplied cause attached.
// The original sentinel is not mutated.
func (e *AppError) WithWrap(cause error) *AppError {
	cp := *e
	cp.Wrapped = cause
	return &cp
}

// WithMessage returns a copy of the error with a more specific message.
// Useful when the same code applies but the context is more specific
// (e.g. ErrInvalidAmount with "amount must be positive").
func (e *AppError) WithMessage(msg string) *AppError {
	cp := *e
	cp.Message = msg
	return &cp
}

// As is a convenience wrapper to extract an *AppError from any error chain.
func As(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
