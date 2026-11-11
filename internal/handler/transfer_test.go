package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"wallet-transfer-assignment/internal/appErrors"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

func TestTransfer_InvalidInputNeverReachesService(t *testing.T) {
	amount := func(value string) string { return strings.Replace(validBody, `"amount":100`, `"amount":`+value, 1) }
	tests := map[string]string{
		"empty":          "",
		"null":           "null",
		"array":          "[]",
		"malformed JSON": "{",
		"extra object":   validBody + ` {}`,
		"extra null":     validBody + ` null`,
		"zero amount":    amount("0"),
		"above limit":    amount("1001"),
		"fraction":       amount("1.5"),
		"exponent":       amount("1e2"),
		"int64 overflow": amount("9223372036854775808"),
		"string amount":  amount(`"100"`),
		"null amount":    amount("null"),
		"unknown field":  amount(`100,"unknown":true`),
		"same wallets":   strings.Replace(validBody, "wallet_2", "wallet_1", 1),
		"oversized body": validBody + strings.Repeat(" ", 4096),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			response := f.post(body)
			f.wantError(response, http.StatusBadRequest, "VALIDATION_ERROR")
			if f.service.calls != 0 {
				t.Fatal("invalid input reached the service")
			}
		})
	}
}

func TestTransfer_ReturnsServiceResultAndPreservesRequest(t *testing.T) {
	f := setup(t)
	response := f.post(validBody)

	f.wantResponse(response, http.StatusCreated, string(f.service.result.Body))
	want := domain.TransferRequest{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	if f.service.calls != 1 || f.service.request != want {
		t.Fatalf("calls=%d request=%+v, want one call with %+v", f.service.calls, f.service.request, want)
	}
	if response.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("first response must not be marked as replayed")
	}
}

func TestTransfer_ReplaysOriginalResponseBytes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"processed", http.StatusCreated, "{\n  \"transferId\":\"T1\",\"status\":\"PROCESSED\"\n}"},
		{"failed", http.StatusUnprocessableEntity, `{"transferId":"T1","status":"FAILED","failureReason":"INSUFFICIENT_FUNDS"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.service.result = service.TransferResult{StatusCode: tc.status, Body: []byte(tc.body), Replayed: true}
			response := f.post(validBody)

			f.wantResponse(response, tc.status, tc.body)
			if response.Header().Get("Idempotent-Replayed") != "true" {
				t.Fatal("replay header missing")
			}
		})
	}
}

func TestTransfer_PendingResponseIncludesRetryDelay(t *testing.T) {
	f := setup(t)
	body := `{"transferId":"T1","status":"PENDING"}`
	f.service.result = service.TransferResult{StatusCode: http.StatusAccepted, Body: []byte(body)}
	response := f.post(validBody)

	f.wantResponse(response, http.StatusAccepted, body)
	if response.Header().Get("Retry-After") != "1" {
		t.Fatal("pending response must include retry delay")
	}
	if response.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("pending response is not a terminal replay")
	}
}

func TestTransfer_MapsServiceErrorsWithoutExposingDetails(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{appErrors.ErrInvalidTransfer, 400, "VALIDATION_ERROR"},
		{appErrors.ErrWalletNotFound, 404, "WALLET_NOT_FOUND"},
		{appErrors.ErrIdempotencyConflict, 409, "IDEMPOTENCY_KEY_REUSED"},
		{appErrors.ErrInsufficientFunds, 422, "INSUFFICIENT_FUNDS"},
		{appErrors.ErrServiceUnavailable, 503, "SERVICE_UNAVAILABLE"},
		{appErrors.ErrTransferTemporarilyUnavailable, 503, "TRANSFER_TEMPORARILY_UNAVAILABLE"},
		{context.DeadlineExceeded, 503, "TRANSFER_TEMPORARILY_UNAVAILABLE"},
		{errors.New("postgres://user:password"), 500, "INTERNAL_ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.err.Error(), func(t *testing.T) {
			f := setup(t)
			f.service.err = fmt.Errorf("private cause: %w", tc.err)
			response := f.post(validBody)

			f.wantError(response, tc.status, tc.code)
			if strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "password") {
				t.Fatal("internal error details leaked into response")
			}
		})
	}
}

func TestTransfer_JSONFormattingDoesNotChangeRequestHash(t *testing.T) {
	f := setup(t)
	f.post(validBody)
	first := f.service.request.Hash()
	response := f.post(` { "amount":100,"toWalletId":"wallet_2","fromWalletId":"wallet_1","idempotencyKey":"abc123" } `)

	f.wantResponse(response, http.StatusCreated, string(f.service.result.Body))
	if f.service.calls != 2 || first != f.service.request.Hash() {
		t.Fatal("JSON representation changed the request hash")
	}
}

func TestTransfer_EnforcesBodySizeLimit(t *testing.T) {
	tests := []struct {
		name                string
		size, status, calls int
	}{
		{"at limit", 4096, http.StatusCreated, 1},
		{"above limit", 4097, http.StatusBadRequest, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			response := f.post(validBody + strings.Repeat(" ", tc.size-len(validBody)))
			if response.Code != tc.status || f.service.calls != tc.calls {
				t.Fatalf("status=%d calls=%d, want %d and %d", response.Code, f.service.calls, tc.status, tc.calls)
			}
		})
	}
}

func TestTransfer_InvalidServiceResponseReturnsInternalError(t *testing.T) {
	f := setup(t)
	f.service.result = service.TransferResult{}
	f.wantError(f.post(validBody), http.StatusInternalServerError, "INTERNAL_ERROR")
}
