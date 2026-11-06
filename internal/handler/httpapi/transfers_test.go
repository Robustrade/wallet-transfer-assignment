package httpapi_test

import (
	"bytes"
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
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/handler/httpapi"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// fakeTransfers records the request it received and returns a canned result.
type fakeTransfers struct {
	got    *domain.TransferRequest
	result service.CreateTransferResult
	err    error
	panic  bool
}

func (f *fakeTransfers) CreateTransfer(_ context.Context, req domain.TransferRequest) (service.CreateTransferResult, error) {
	if f.panic {
		panic("boom")
	}
	f.got = &req
	return f.result, f.err
}

func (f *fakeTransfers) GetTransfer(_ context.Context, id uuid.UUID) (domain.Transfer, error) {
	if f.err != nil {
		return domain.Transfer{}, f.err
	}
	if id != f.result.Transfer.ID {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return f.result.Transfer, nil
}

type fakeWallets struct{ wallets map[string]domain.Wallet }

func (f fakeWallets) GetWallet(_ context.Context, id string) (domain.Wallet, error) {
	w, ok := f.wallets[id]
	if !ok {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return w, nil
}

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

var now = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

func transfer(status domain.TransferStatus, reason domain.FailureReason) domain.Transfer {
	return domain.Transfer{
		ID: uuid.MustParse("6f1c2d3e-0000-4000-8000-000000000001"), IdempotencyKey: "abc123",
		FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
		Status: status, FailureReason: reason, CreatedAt: now, UpdatedAt: now,
	}
}

func newServer(svc *fakeTransfers, pinger httpapi.Pinger) http.Handler {
	return httpapi.NewRouter(httpapi.Deps{
		Transfers: svc,
		Wallets: fakeWallets{wallets: map[string]domain.Wallet{
			"wallet_1": {ID: "wallet_1", Balance: 900, CreatedAt: now, UpdatedAt: now},
		}},
		Readiness: pinger,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func do(t *testing.T, h http.Handler, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	errObj, ok := decode(t, rec)["error"].(map[string]any)
	require.True(t, ok, "expected an error envelope, got %s", rec.Body.String())
	return errObj["code"].(string)
}

const validBody = `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

func TestCreateTransfer_ProcessedReturns201WithTheTransfer(t *testing.T) {
	svc := &fakeTransfers{result: service.CreateTransferResult{Transfer: transfer(domain.StatusProcessed, "")}}

	rec := do(t, newServer(svc, nil), http.MethodPost, "/transfers", validBody)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "/transfers/6f1c2d3e-0000-4000-8000-000000000001", rec.Header().Get("Location"))
	require.Empty(t, rec.Header().Get("Idempotent-Replayed"))
	require.JSONEq(t, `{
		"id":"6f1c2d3e-0000-4000-8000-000000000001","idempotencyKey":"abc123",
		"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100,
		"status":"PROCESSED","failureReason":null,
		"createdAt":"2026-11-04T10:00:00Z","updatedAt":"2026-11-04T10:00:00Z"}`, rec.Body.String())
	require.Equal(t, &domain.TransferRequest{
		IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	}, svc.got)
}

func TestCreateTransfer_ReplayKeepsTheOriginalStatusAndMarksTheResponse(t *testing.T) {
	svc := &fakeTransfers{result: service.CreateTransferResult{Transfer: transfer(domain.StatusProcessed, ""), Replayed: true}}

	rec := do(t, newServer(svc, nil), http.MethodPost, "/transfers", validBody)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "true", rec.Header().Get("Idempotent-Replayed"))
}

func TestCreateTransfer_InsufficientFundsReturns422WithTheFailedTransfer(t *testing.T) {
	svc := &fakeTransfers{result: service.CreateTransferResult{
		Transfer: transfer(domain.StatusFailed, domain.FailureInsufficientFunds),
	}}

	rec := do(t, newServer(svc, nil), http.MethodPost, "/transfers", validBody)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, "INSUFFICIENT_FUNDS", errorCode(t, rec))
	tr := decode(t, rec)["transfer"].(map[string]any)
	require.Equal(t, "FAILED", tr["status"])
	require.Equal(t, "INSUFFICIENT_FUNDS", tr["failureReason"])
}

func TestCreateTransfer_RejectsMalformedRequestsWithoutCallingTheService(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid JSON", `{"idempotencyKey":`},
		{"not an object", `[1,2,3]`},
		{"fractional amount", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":10.5}`},
		{"exponent amount", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1e2}`},
		{"string amount", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":"100"}`},
		{"amount overflows int64", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":9223372036854775808}`},
		{"missing amount", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b"}`},
		{"unknown field", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1,"currency":"USD"}`},
		{"trailing data", validBody + `{}`},
		{"wallet id is a number", `{"idempotencyKey":"k","fromWalletId":1,"toWalletId":"b","amount":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeTransfers{}

			rec := do(t, newServer(svc, nil), http.MethodPost, "/transfers", tc.body)

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Equal(t, "VALIDATION_ERROR", errorCode(t, rec))
			require.Nil(t, svc.got, "the service must not be called")
		})
	}
}

func TestCreateTransfer_RejectsOversizedBodies(t *testing.T) {
	svc := &fakeTransfers{}
	body := `{"idempotencyKey":"` + strings.Repeat("k", 2<<20) + `"}`

	rec := do(t, newServer(svc, nil), http.MethodPost, "/transfers", body)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Nil(t, svc.got)
}

func TestCreateTransfer_MapsServiceErrorsToStatusCodes(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{&domain.ValidationError{Field: "amount", Message: "must be a positive integer"}, http.StatusBadRequest, "VALIDATION_ERROR"},
		{fmt.Errorf("wallet x: %w", domain.ErrWalletNotFound), http.StatusNotFound, "WALLET_NOT_FOUND"},
		{domain.ErrIdempotencyKeyReused, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED"},
		{fmt.Errorf("%w: lock timeout", domain.ErrBusy), http.StatusServiceUnavailable, "SERVICE_BUSY"},
		{errors.New("connection reset by peer"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.wantCode, func(t *testing.T) {
			rec := do(t, newServer(&fakeTransfers{err: tc.err}, nil), http.MethodPost, "/transfers", validBody)

			require.Equal(t, tc.wantStatus, rec.Code)
			require.Equal(t, tc.wantCode, errorCode(t, rec))
		})
	}
}

func TestCreateTransfer_InternalErrorsDoNotLeakDetails(t *testing.T) {
	svc := &fakeTransfers{err: errors.New("pq: password authentication failed for user wallet")}

	rec := do(t, newServer(svc, nil), http.MethodPost, "/transfers", validBody)

	require.NotContains(t, rec.Body.String(), "password")
}

func TestBusyResponsesTellClientsWhenToRetry(t *testing.T) {
	rec := do(t, newServer(&fakeTransfers{err: domain.ErrBusy}, nil), http.MethodPost, "/transfers", validBody)

	require.Equal(t, "1", rec.Header().Get("Retry-After"))
}

func TestMiddleware_RecoversFromPanicsWith500(t *testing.T) {
	rec := do(t, newServer(&fakeTransfers{panic: true}, nil), http.MethodPost, "/transfers", validBody)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "INTERNAL_ERROR", errorCode(t, rec))
}

func TestMiddleware_PropagatesOrGeneratesRequestIDs(t *testing.T) {
	h := newServer(&fakeTransfers{}, nil)

	rec := do(t, h, http.MethodGet, "/healthz", "", "X-Request-ID", "req-123")
	require.Equal(t, "req-123", rec.Header().Get("X-Request-ID"))

	rec = do(t, h, http.MethodGet, "/healthz", "")
	require.NotEmpty(t, rec.Header().Get("X-Request-ID"))

	rec = do(t, h, http.MethodGet, "/healthz", "", "X-Request-ID", strings.Repeat("x", 500))
	require.NotEqual(t, strings.Repeat("x", 500), rec.Header().Get("X-Request-ID"), "oversized ids are replaced")
}

func TestHealthEndpoints(t *testing.T) {
	rec := do(t, newServer(&fakeTransfers{}, fakePinger{}), http.MethodGet, "/healthz", "")
	require.Equal(t, http.StatusOK, rec.Code)

	rec = do(t, newServer(&fakeTransfers{}, fakePinger{}), http.MethodGet, "/readyz", "")
	require.Equal(t, http.StatusOK, rec.Code)

	rec = do(t, newServer(&fakeTransfers{}, fakePinger{err: errors.New("db down")}), http.MethodGet, "/readyz", "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestUnknownRoutesAndMethods(t *testing.T) {
	h := newServer(&fakeTransfers{}, nil)

	require.Equal(t, http.StatusNotFound, do(t, h, http.MethodGet, "/nope", "").Code)
	require.Equal(t, http.StatusMethodNotAllowed, do(t, h, http.MethodDelete, "/transfers", "").Code)
}

// Guard against accidental buffering bugs: the body must be valid JSON even for errors.
func TestErrorBodiesAreJSON(t *testing.T) {
	rec := do(t, newServer(&fakeTransfers{}, nil), http.MethodPost, "/transfers", `{`)
	require.True(t, json.Valid(bytes.TrimSpace(rec.Body.Bytes())))
}
