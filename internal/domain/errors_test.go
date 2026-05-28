package domain

import (
	"errors"
	"testing"
)

// TestErrors_AreDistinctSentinels guards against the bug where
// someone copies an error declaration and ends up with two
// variables aliasing the same value — that would make
// errors.Is(ErrSameWallet, ErrInvalidAmount) return true and
// silently corrupt the handler classifier.
func TestErrors_AreDistinctSentinels(t *testing.T) {
	all := []error{
		ErrWalletNotFound,
		ErrInsufficientFunds,
		ErrSameWallet,
		ErrInvalidAmount,
		ErrInvalidIdempotencyKey,
		ErrIdempotencyConflict,
		ErrInvalidStateTransition,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Fatalf("sentinels collide: %v matches %v", a, b)
			}
		}
	}
}

// TestErrors_HaveNonEmptyMessages catches the regression where a
// sentinel is declared with errors.New("") — a blank message would
// silently propagate through the API as `{"error":""}`.
func TestErrors_HaveNonEmptyMessages(t *testing.T) {
	for _, e := range []error{
		ErrWalletNotFound, ErrInsufficientFunds, ErrSameWallet,
		ErrInvalidAmount, ErrInvalidIdempotencyKey, ErrIdempotencyConflict,
		ErrInvalidStateTransition,
	} {
		if e.Error() == "" {
			t.Fatalf("empty message on %T", e)
		}
	}
}

// TestErrors_WrappingPreservesIs is the contract test that the
// handler relies on: a wrapped sentinel still matches via
// errors.Is. Without this, wrapping at any layer would silently
// break the classify mapping.
func TestErrors_WrappingPreservesIs(t *testing.T) {
	wrapped := errorsWrap("op: %w", ErrIdempotencyConflict)
	if !errors.Is(wrapped, ErrIdempotencyConflict) {
		t.Fatal("errors.Is broken across wrap")
	}
}

// errorsWrap is a tiny helper so the test reads naturally; the
// real test only cares that Unwrap on the returned value still
// reaches the inner sentinel.
func errorsWrap(format string, err error) error {
	return wrappedErr{inner: err, msg: format}
}

type wrappedErr struct {
	inner error
	msg   string
}

func (w wrappedErr) Error() string { return w.msg + ": " + w.inner.Error() }
func (w wrappedErr) Unwrap() error { return w.inner }
