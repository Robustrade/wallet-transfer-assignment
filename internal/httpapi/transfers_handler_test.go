package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/httpapi"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

// fakeTransferService lets handler tests exercise every branch in the
// status-code map without a database.
type fakeTransferService struct {
	createResult service.TransferResult
	createErr    error
	getResult    domain.Transfer
	getErr       error
	lastCmd      domain.TransferCommand
}

func (f *fakeTransferService) Create(_ context.Context, cmd domain.TransferCommand) (service.TransferResult, error) {
	f.lastCmd = cmd
	return f.createResult, f.createErr
}

func (f *fakeTransferService) Get(_ context.Context, _ uuid.UUID) (domain.Transfer, error) {
	return f.getResult, f.getErr
}

type fakeWalletService struct{}

func (fakeWalletService) Create(context.Context, string, string) (domain.Wallet, error) {
	return domain.Wallet{}, nil
}
func (fakeWalletService) Get(context.Context, string) (domain.Wallet, error) {
	return domain.Wallet{}, nil
}
func (fakeWalletService) ListEntries(context.Context, string, int) ([]domain.LedgerEntry, error) {
	return nil, nil
}

type fakeHealthChecker struct{ err error }

func (f fakeHealthChecker) Ping(context.Context) error { return f.err }

func newTestServer(transfers httpapi.TransferService) *httptest.Server {
	router := httpapi.NewRouter(transfers, fakeWalletService{}, fakeHealthChecker{}, testLogger())
	return httptest.NewServer(router)
}

func postJSON(t *testing.T, url string, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

func TestTransfersHandler_Create_AssignmentExampleBody(t *testing.T) {
	fake := &fakeTransferService{
		createResult: service.TransferResult{
			Outcome: service.OutcomeCreated,
			Transfer: domain.Transfer{
				ID:             uuid.Must(uuid.NewV7()),
				IdempotencyKey: "abc123",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				AmountMinor:    100,
				Currency:       "INR",
				Status:         domain.TransferStatusProcessed,
				CreatedAt:      time.Now(),
				SettledAt:      timePtr(time.Now()),
			},
		},
	}
	srv := newTestServer(fake)
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	body := decodeBody(t, resp)
	require.Equal(t, "abc123", body["idempotencyKey"])
	require.Equal(t, float64(100), body["amount"])
	require.Equal(t, "PROCESSED", body["status"])
	require.Empty(t, resp.Header.Get("Idempotent-Replay"))

	require.Equal(t, "abc123", fake.lastCmd.IdempotencyKey)
	require.Equal(t, int64(100), fake.lastCmd.AmountMinor)
}

func TestTransfersHandler_Create_FailedSettlementIs422(t *testing.T) {
	fake := &fakeTransferService{
		createResult: service.TransferResult{
			Outcome: service.OutcomeCreated,
			Transfer: domain.Transfer{
				ID:             uuid.Must(uuid.NewV7()),
				IdempotencyKey: "abc123",
				Status:         domain.TransferStatusFailed,
				FailureCode:    domain.FailureCodeInsufficientFunds,
				FailureMessage: "insufficient funds",
				CreatedAt:      time.Now(),
			},
		},
	}
	srv := newTestServer(fake)
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	body := decodeBody(t, resp)
	require.Equal(t, "INSUFFICIENT_FUNDS", body["failureCode"])
}

func TestTransfersHandler_Create_ReplaySetsHeader(t *testing.T) {
	fake := &fakeTransferService{
		createResult: service.TransferResult{
			Outcome: service.OutcomeReplayed,
			Transfer: domain.Transfer{
				ID:             uuid.Must(uuid.NewV7()),
				IdempotencyKey: "abc123",
				Status:         domain.TransferStatusProcessed,
				CreatedAt:      time.Now(),
				SettledAt:      timePtr(time.Now()),
			},
		},
	}
	srv := newTestServer(fake)
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "true", resp.Header.Get("Idempotent-Replay"))
}

func TestTransfersHandler_Create_StatusCodeMap(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"wallet not found", domain.ErrWalletNotFound, http.StatusNotFound, "WALLET_NOT_FOUND"},
		{"idempotency key reuse", domain.ErrIdempotencyKeyReuse, http.StatusConflict, "IDEMPOTENCY_KEY_REUSE"},
		{"currency mismatch", domain.ErrCurrencyMismatch, http.StatusConflict, "CURRENCY_MISMATCH"},
		{"transfer in progress", domain.ErrTransferInProgress, http.StatusConflict, "TRANSFER_IN_PROGRESS"},
		{"resource busy", domain.ErrResourceBusy, http.StatusServiceUnavailable, "RESOURCE_BUSY"},
		{"validation error", domain.NewValidationError("amount", "amount must be greater than zero"), http.StatusBadRequest, "VALIDATION_ERROR"},
		{"unexpected error", errors.New("boom"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTransferService{createErr: tc.err}
			srv := newTestServer(fake)
			defer srv.Close()

			resp := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
			require.Equal(t, tc.wantStatus, resp.StatusCode)
			body := decodeBody(t, resp)
			errBody, ok := body["error"].(map[string]any)
			require.True(t, ok, "expected an error envelope, got %v", body)
			require.Equal(t, tc.wantCode, errBody["code"])

			if tc.wantStatus == http.StatusServiceUnavailable {
				require.Equal(t, "1", resp.Header.Get("Retry-After"))
			}
		})
	}
}

func TestTransfersHandler_Create_MalformedBodies(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not json", `not json at all`},
		{"unknown field", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":100,"bogus":1}`},
		{"amount is a float", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":100.5}`},
		{"amount is a string", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":"100"}`},
		{"trailing garbage", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":100}{}`},
		{"empty body", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTransferService{}
			srv := newTestServer(fake)
			defer srv.Close()

			resp := postJSON(t, srv.URL+"/transfers", tc.body)
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			body := decodeBody(t, resp)
			errBody := body["error"].(map[string]any)
			require.Equal(t, "MALFORMED_REQUEST", errBody["code"])
		})
	}
}

func TestTransfersHandler_Create_OversizedBodyRejected(t *testing.T) {
	fake := &fakeTransferService{}
	srv := newTestServer(fake)
	defer srv.Close()

	huge := `{"idempotencyKey":"` + strings.Repeat("a", 100_000) + `","fromWalletId":"a","toWalletId":"b","amount":100}`
	resp, err := http.Post(srv.URL+"/transfers", "application/json", bytes.NewReader([]byte(huge)))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestTransfersHandler_Create_MissingIdempotencyKey(t *testing.T) {
	fake := &fakeTransferService{}
	srv := newTestServer(fake)
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/transfers", `{"fromWalletId":"a","toWalletId":"b","amount":100}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeBody(t, resp)
	errBody := body["error"].(map[string]any)
	require.Equal(t, "VALIDATION_ERROR", errBody["code"])
	require.Equal(t, "idempotencyKey", errBody["field"])
}

func TestTransfersHandler_Create_IdempotencyKeyViaHeader(t *testing.T) {
	fake := &fakeTransferService{
		createResult: service.TransferResult{
			Outcome:  service.OutcomeCreated,
			Transfer: domain.Transfer{ID: uuid.Must(uuid.NewV7()), Status: domain.TransferStatusProcessed, CreatedAt: time.Now(), SettledAt: timePtr(time.Now())},
		},
	}
	srv := newTestServer(fake)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/transfers", strings.NewReader(`{"fromWalletId":"a","toWalletId":"b","amount":100}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "from-header")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "from-header", fake.lastCmd.IdempotencyKey)
}

func TestTransfersHandler_Create_ConflictingKeySources(t *testing.T) {
	fake := &fakeTransferService{}
	srv := newTestServer(fake)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/transfers", strings.NewReader(`{"idempotencyKey":"body-key","fromWalletId":"a","toWalletId":"b","amount":100}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "header-key")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestTransfersHandler_Get(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	fake := &fakeTransferService{getResult: domain.Transfer{ID: id, Status: domain.TransferStatusProcessed, CreatedAt: time.Now()}}
	srv := newTestServer(fake)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/transfers/" + id.String())
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decodeBody(t, resp)
	require.Equal(t, id.String(), body["id"])
}

func TestTransfersHandler_Get_NotFound(t *testing.T) {
	fake := &fakeTransferService{getErr: domain.ErrTransferNotFound}
	srv := newTestServer(fake)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/transfers/" + uuid.Must(uuid.NewV7()).String())
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func timePtr(t time.Time) *time.Time { return &t }
