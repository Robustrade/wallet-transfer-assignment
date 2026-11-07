package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/handler"
	"github.com/celinagupta/wallet-transfer-assignment/internal/service"
)

// fakeService records the request it receives and returns a canned result.
type fakeService struct {
	called bool
	got    domain.TransferRequest
	result service.Result
	err    error
}

func (f *fakeService) CreateTransfer(_ context.Context, req domain.TransferRequest) (service.Result, error) {
	f.called, f.got = true, req
	return f.result, f.err
}

func post(t *testing.T, svc *fakeService, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.NewTransferHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body)))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), "body: %s", rec.Body.String())
	return out
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := decode(t, rec)
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok, "want an error object, got %v", body)
	return errObj["code"].(string)
}

const validBody = `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

var createdAt = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

func storedTransfer(status domain.TransferStatus, reason domain.FailureReason) domain.Transfer {
	return domain.Transfer{
		ID:             uuid.MustParse("6f1c2b8e-8a49-4b2e-9a51-0c7b3d0e1f00"),
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
		Status:         status,
		FailureReason:  reason,
		CreatedAt:      createdAt,
	}
}

func TestCreateTransferPassesRequestToService(t *testing.T) {
	svc := &fakeService{result: service.Result{Transfer: storedTransfer(domain.StatusProcessed, "")}}

	post(t, svc, validBody)

	assert.Equal(t, domain.TransferRequest{
		IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	}, svc.got)
}

func TestCreateTransferWithoutKeyPassesEmptyKey(t *testing.T) {
	svc := &fakeService{result: service.Result{Transfer: storedTransfer(domain.StatusProcessed, "")}}

	rec := post(t, svc, `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Empty(t, svc.got.IdempotencyKey)
}

func TestProcessedTransferReturns201(t *testing.T) {
	svc := &fakeService{result: service.Result{Transfer: storedTransfer(domain.StatusProcessed, "")}}

	rec := post(t, svc, validBody)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Empty(t, rec.Header().Get("Idempotent-Replayed"))
	assert.Equal(t, map[string]any{
		"id":             "6f1c2b8e-8a49-4b2e-9a51-0c7b3d0e1f00",
		"idempotencyKey": "abc123",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         float64(100),
		"status":         "PROCESSED",
		"failureReason":  nil,
		"createdAt":      "2026-11-07T10:00:00Z",
	}, decode(t, rec))
}

func TestInsufficientFundsReturns422WithFailedTransfer(t *testing.T) {
	svc := &fakeService{result: service.Result{
		Transfer: storedTransfer(domain.StatusFailed, domain.FailureInsufficientFunds),
	}}

	rec := post(t, svc, validBody)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := decode(t, rec)
	assert.Equal(t, "FAILED", body["status"])
	assert.Equal(t, "INSUFFICIENT_FUNDS", body["failureReason"])
}

func TestReplayReturnsOriginalStatusWithReplayHeader(t *testing.T) {
	tests := map[string]struct {
		transfer   domain.Transfer
		wantStatus int
	}{
		"replayed success": {storedTransfer(domain.StatusProcessed, ""), http.StatusCreated},
		"replayed failure": {storedTransfer(domain.StatusFailed, domain.FailureInsufficientFunds), http.StatusUnprocessableEntity},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			svc := &fakeService{result: service.Result{Transfer: tc.transfer, Replayed: true}}

			rec := post(t, svc, validBody)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "true", rec.Header().Get("Idempotent-Replayed"))
			assert.Equal(t, string(tc.transfer.Status), decode(t, rec)["status"])
		})
	}
}

func TestMalformedRequestsAreRejectedBeforeTheService(t *testing.T) {
	tests := map[string]string{
		"not json":              `not json`,
		"empty body":            ``,
		"unknown field":         `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100,"currency":"USD"}`,
		"fractional amount":     `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100.5}`,
		"amount as string":      `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":"100"}`,
		"amount overflows":      `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":99999999999999999999}`,
		"explicit empty key":    `{"idempotencyKey":"","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`,
		"trailing data":         validBody + `{}`,
		"array instead of body": `[]`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			svc := &fakeService{}

			rec := post(t, svc, body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "VALIDATION_ERROR", errorCode(t, rec))
			assert.False(t, svc.called, "service must not be called for a malformed request")
		})
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	svc := &fakeService{}
	body := `{"idempotencyKey":"` + strings.Repeat("k", 2<<20) + `","fromWalletId":"a","toWalletId":"b","amount":1}`

	rec := post(t, svc, body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.False(t, svc.called)
}

func TestServiceErrorsMapToStatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantErr  string
	}{
		{"validation", &domain.ValidationError{Field: "amount", Message: "must be positive"}, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"unknown wallet", fmt.Errorf("source %q: %w", "ghost", domain.ErrWalletNotFound), http.StatusNotFound, "WALLET_NOT_FOUND"},
		{"key reused", fmt.Errorf("key: %w", domain.ErrIdempotencyKeyReused), http.StatusConflict, "IDEMPOTENCY_KEY_REUSED"},
		{"retryable", fmt.Errorf("lock: %w", domain.ErrTemporarilyUnavailable), http.StatusServiceUnavailable, "RETRYABLE"},
		{"unexpected", errors.New("connection reset by peer"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, &fakeService{err: tc.err}, validBody)

			assert.Equal(t, tc.wantCode, rec.Code)
			assert.Equal(t, tc.wantErr, errorCode(t, rec))
		})
	}
}

func TestValidationErrorNamesTheField(t *testing.T) {
	svc := &fakeService{err: &domain.ValidationError{Field: "amount", Message: "must be positive"}}

	rec := post(t, svc, validBody)

	assert.Contains(t, rec.Body.String(), "amount: must be positive")
}

func TestRetryableErrorSuggestsRetryAfter(t *testing.T) {
	rec := post(t, &fakeService{err: domain.ErrTemporarilyUnavailable}, validBody)

	assert.Equal(t, "1", rec.Header().Get("Retry-After"))
}

func TestInternalErrorDoesNotLeakDetails(t *testing.T) {
	rec := post(t, &fakeService{err: errors.New("pq: password authentication failed for user wallet")}, validBody)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "password")
}
