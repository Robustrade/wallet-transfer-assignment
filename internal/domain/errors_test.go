package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFailureReasonFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantReason FailureReason
		wantOK     bool
	}{
		{"insufficient funds", ErrInsufficientFunds, ReasonInsufficientFunds, true},
		{"currency mismatch", ErrCurrencyMismatch, ReasonCurrencyMismatch, true},
		{"balance overflow", ErrBalanceOverflow, ReasonBalanceOverflow, true},
		{"wrapped business error", fmt.Errorf("debit wallet_1: %w", ErrInsufficientFunds), ReasonInsufficientFunds, true},
		// Everything else is not a business-rule outcome and must not be
		// stored as a FAILED transfer.
		{"validation", ErrValidation, "", false},
		{"wallet not found", ErrWalletNotFound, "", false},
		{"invalid transition", ErrInvalidTransition, "", false},
		{"key reused", ErrIdempotencyKeyReused, "", false},
		{"arbitrary error", errors.New("boom"), "", false},
		{"nil", nil, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reason, ok := FailureReasonFor(tc.err)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantReason, reason)
		})
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	all := []error{
		ErrValidation, ErrWalletNotFound, ErrInsufficientFunds, ErrCurrencyMismatch,
		ErrBalanceOverflow, ErrInvalidTransition, ErrIdempotencyKeyReused,
	}
	for i, a := range all {
		for j, b := range all {
			if i != j {
				assert.NotErrorIs(t, a, b, "%v should not match %v", a, b)
			}
		}
	}
}
