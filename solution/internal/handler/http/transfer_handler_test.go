package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
	handlerhttp "github.com/heisenberglit/wallet-transfer-assignment/internal/handler/http"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/service"
)

type fakeCreator struct {
	transfer *domain.Transfer
	err      error
	called   bool
	gotInput service.CreateTransferInput
}

func (f *fakeCreator) CreateTransfer(_ context.Context, in service.CreateTransferInput) (*domain.Transfer, error) {
	f.called = true
	f.gotInput = in
	return f.transfer, f.err
}

func post(t *testing.T, creator *fakeCreator, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := handlerhttp.NewRouter(handlerhttp.NewTransferHandler(creator))
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

const validBody = `{"idempotencyKey":"key-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

func TestCreate_Success(t *testing.T) {
	creator := &fakeCreator{transfer: &domain.Transfer{
		ID: "t-1", IdempotencyKey: "key-1", FromWalletID: "wallet_1",
		ToWalletID: "wallet_2", Amount: 100, State: domain.TransferProcessed,
	}}

	rec := post(t, creator, validBody)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "t-1", got["id"])
	assert.Equal(t, "PROCESSED", got["state"])
	assert.EqualValues(t, 100, got["amount"])

	assert.Equal(t, service.CreateTransferInput{
		IdempotencyKey: "key-1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	}, creator.gotInput, "handler must pass the decoded request through unchanged")
}

// The regression this suite was missing: a replayed FAILED transfer must not
// be reported to the client as a 201 success.
func TestCreate_FailedTransferIsNotReportedAsSuccess(t *testing.T) {
	creator := &fakeCreator{
		transfer: &domain.Transfer{ID: "t-1", State: domain.TransferFailed},
		err:      domain.ErrInsufficientFunds,
	}

	rec := post(t, creator, validBody)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.NotContains(t, rec.Body.String(), "PROCESSED")
}

func TestCreate_ServiceErrorsMapToStatusCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"insufficient funds", domain.ErrInsufficientFunds, http.StatusUnprocessableEntity},
		{"invalid amount", domain.ErrInvalidAmount, http.StatusUnprocessableEntity},
		{"same wallet", domain.ErrSameWallet, http.StatusUnprocessableEntity},
		{"wallet not found", domain.ErrWalletNotFound, http.StatusNotFound},
		{"idempotency conflict", domain.ErrIdempotencyConflict, http.StatusConflict},
		{"transfer still in progress", domain.ErrTransferInProgress, http.StatusConflict},
		{"unexpected error", errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, &fakeCreator{err: tc.err}, validBody)
			assert.Equal(t, tc.want, rec.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.NotEmpty(t, body["error"])
		})
	}
}

// An unexpected error must not leak its internal message to the client.
func TestCreate_UnexpectedErrorIsNotLeaked(t *testing.T) {
	rec := post(t, &fakeCreator{err: errors.New("pq: connection refused on 10.0.0.7")}, validBody)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "10.0.0.7")
	assert.Contains(t, rec.Body.String(), "internal error")
}

func TestCreate_RejectsBadRequestsWithoutCallingTheService(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"idempotencyKey":`},
		{"empty body", ``},
		{"two json objects", validBody + validBody},
		{"trailing junk after a valid object", validBody + "garbage"},
		{"missing idempotency key", `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`},
		{"missing from wallet", `{"idempotencyKey":"key-1","toWalletId":"wallet_2","amount":100}`},
		{"missing to wallet", `{"idempotencyKey":"key-1","fromWalletId":"wallet_1","amount":100}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			creator := &fakeCreator{}
			rec := post(t, creator, tc.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.False(t, creator.called, "service must not be reached for an invalid request")
		})
	}
}

// Amount validation belongs to the service, not the handler — the handler
// must forward it rather than silently deciding on its own.
func TestCreate_ForwardsAmountValidationToService(t *testing.T) {
	creator := &fakeCreator{err: domain.ErrInvalidAmount}
	body := `{"idempotencyKey":"key-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":-5}`

	rec := post(t, creator, body)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.True(t, creator.called)
	assert.EqualValues(t, -5, creator.gotInput.Amount)
}

func TestHealthz(t *testing.T) {
	router := handlerhttp.NewRouter(handlerhttp.NewTransferHandler(&fakeCreator{}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestRouter_RejectsWrongMethodAndUnknownPath(t *testing.T) {
	router := handlerhttp.NewRouter(handlerhttp.NewTransferHandler(&fakeCreator{}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/transfers", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nope", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
