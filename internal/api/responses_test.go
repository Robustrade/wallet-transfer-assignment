package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"wallet/internal/constants"
	"wallet/internal/domain"
)

// TestErrorResponseMapping pins the single error-mapping table, including the
// wrapped forms errors actually arrive in: sentinels wrapped with %w by the
// storage/service layers, and *domain.ValidationError matched with errors.As.
func TestErrorResponseMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			"validation error",
			domain.NewValidationError("bad %s", "field"),
			http.StatusBadRequest, constants.CodeValidation,
		},
		{
			"wrapped validation error",
			fmt.Errorf("decode: %w", domain.NewValidationError("bad field")),
			http.StatusBadRequest, constants.CodeValidation,
		},
		{
			"wallet not found",
			fmt.Errorf("%w: wallet_9", domain.ErrWalletNotFound),
			http.StatusNotFound, constants.CodeWalletNotFound,
		},
		{
			"transfer not found",
			domain.ErrTransferNotFound,
			http.StatusNotFound, constants.CodeTransferNotFound,
		},
		{
			"key reused",
			fmt.Errorf("%w: key k", domain.ErrKeyReused),
			http.StatusConflict, constants.CodeKeyReused,
		},
		{
			"insufficient funds",
			fmt.Errorf("%w: balance 10, requested 100", domain.ErrInsufficientFunds),
			http.StatusUnprocessableEntity, constants.CodeInsufficientFunds,
		},
		{
			"lock timeout",
			fmt.Errorf("%w: 55P03", domain.ErrLockTimeout),
			http.StatusServiceUnavailable, constants.CodeLockTimeout,
		},
		{
			"statement timeout",
			fmt.Errorf("%w: 57014", domain.ErrStatementTimeout),
			http.StatusServiceUnavailable, constants.CodeStatementTimeout,
		},
		{
			"database unavailable",
			fmt.Errorf("%w: dial tcp 127.0.0.1:5433: connect: connection refused", domain.ErrDatabaseUnavailable),
			http.StatusServiceUnavailable, constants.CodeDatabaseUnavailable,
		},
		{
			// The exact shape transfer.go builds for an unresolved commit.
			"commit outcome unknown",
			fmt.Errorf("%w: %w", domain.ErrCommitOutcomeUnknown, errors.New("commit failed")),
			http.StatusServiceUnavailable, constants.CodeOutcomeUnknown,
		},
		{
			// A client that hangs up mid-request is not a server fault: the
			// mapping must not pretend it is a 500.
			"client cancelled request",
			fmt.Errorf("claim idempotency key: %w", context.Canceled),
			constants.ClientClosedRequest, constants.CodeClientClosed,
		},
		{
			"incomplete idempotency record",
			fmt.Errorf("%w: key k", domain.ErrIncompleteIdempotency),
			http.StatusInternalServerError, constants.CodeInternal,
		},
		{
			"opaque error",
			errors.New("boom"),
			http.StatusInternalServerError, constants.CodeInternal,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := errorResponse(tc.err)
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if resp.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", resp.Error.Code, tc.wantCode)
			}
			if tc.wantCode == constants.CodeInternal && resp.Error.Message != "internal error" {
				t.Errorf("message = %q, want the fixed no-details message", resp.Error.Message)
			}
		})
	}
}
