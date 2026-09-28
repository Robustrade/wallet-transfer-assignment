package handler_test

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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/handler"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// ---- fakes ----------------------------------------------------------------

type fakeTransfers struct {
	mu    sync.Mutex
	calls []service.CreateTransferCommand
	fn    func(ctx context.Context, cmd service.CreateTransferCommand) (service.TransferResult, error)
}

func (f *fakeTransfers) CreateTransfer(ctx context.Context, cmd service.CreateTransferCommand) (service.TransferResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	f.mu.Unlock()
	return f.fn(ctx, cmd)
}

func (f *fakeTransfers) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeWallets struct {
	getFn   func(ctx context.Context, id string) (*domain.Wallet, error)
	listFn  func(ctx context.Context, id string, limit int) ([]domain.Transfer, error)
	gotList []int // limits passed to ListTransfers
}

func (f *fakeWallets) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return f.getFn(ctx, id)
}

func (f *fakeWallets) ListTransfers(ctx context.Context, id string, limit int) ([]domain.Transfer, error) {
	f.gotList = append(f.gotList, limit)
	return f.listFn(ctx, id, limit)
}

type fakePinger struct{ err error }

func (p fakePinger) PingContext(context.Context) error { return p.err }

// ---- helpers --------------------------------------------------------------

var createdAt = time.Date(2026, 9, 26, 10, 0, 0, 123456000, time.UTC)

func sampleTransfer(status domain.Status, reason domain.FailureReason) domain.Transfer {
	return domain.Transfer{
		ID:             uuid.MustParse("01920000-0000-7000-8000-000000000001"),
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
		Status:         status,
		FailureReason:  reason,
		CreatedAt:      createdAt,
	}
}

func processed(context.Context, service.CreateTransferCommand) (service.TransferResult, error) {
	return service.TransferResult{Transfer: sampleTransfer(domain.StatusProcessed, "")}, nil
}

type testServer struct {
	transfers *fakeTransfers
	wallets   *fakeWallets
	pinger    *fakePinger
	logs      *bytes.Buffer
	router    http.Handler
}

type serverOption func(*handler.RouterConfig)

func newServer(t *testing.T, opts ...serverOption) *testServer {
	t.Helper()
	s := &testServer{
		transfers: &fakeTransfers{fn: processed},
		wallets: &fakeWallets{
			getFn: func(context.Context, string) (*domain.Wallet, error) {
				return &domain.Wallet{ID: "wallet_1", Balance: 500, Currency: "SGD", UpdatedAt: createdAt}, nil
			},
			listFn: func(context.Context, string, int) ([]domain.Transfer, error) { return []domain.Transfer{}, nil },
		},
		pinger: &fakePinger{},
		logs:   &bytes.Buffer{},
	}
	cfg := handler.RouterConfig{
		Transfers:      s.transfers,
		Wallets:        s.wallets,
		DB:             s.pinger,
		Logger:         slog.New(slog.NewJSONHandler(&syncWriter{w: s.logs}, nil)),
		RequestTimeout: 2 * time.Second,
		MaxBodyBytes:   1024,
	}
	for _, o := range opts {
		o(&cfg)
	}
	s.router = handler.NewRouter(cfg)
	return s
}

// syncWriter serializes writes so log assertions are race-free.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *testServer) do(method, path, contentType, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func (s *testServer) postTransfer(body string) *httptest.ResponseRecorder {
	return s.do(http.MethodPost, "/transfers", "application/json", body)
}

const validBody = `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

type errorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var b errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b), rec.Body.String())
	return b
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) errorBody {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	b := decodeError(t, rec)
	assert.Equal(t, code, b.Error.Code)
	assert.NotEmpty(t, b.Error.Message)
	assert.Equal(t, rec.Header().Get("X-Request-ID"), b.Error.RequestID)
	return b
}

// ---- POST /transfers: validation -----------------------------------------

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("k", 129)

	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{"missing idempotencyKey", "", `{"fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"empty idempotencyKey", "", `{"idempotencyKey":"","fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"idempotencyKey too long", "", `{"idempotencyKey":"` + long + `","fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"idempotencyKey with invalid chars", "", `{"idempotencyKey":"a/b","fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"idempotencyKey with whitespace", "", `{"idempotencyKey":"abc 123","fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"idempotencyKey is a number", "", `{"idempotencyKey":123,"fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"idempotencyKey is null", "", `{"idempotencyKey":null,"fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"missing fromWalletId", "", `{"idempotencyKey":"k","toWalletId":"b","amount":1}`},
		{"missing toWalletId", "", `{"idempotencyKey":"k","fromWalletId":"a","amount":1}`},
		{"empty fromWalletId", "", `{"idempotencyKey":"k","fromWalletId":"","toWalletId":"b","amount":1}`},
		{"empty toWalletId", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"","amount":1}`},
		{"from equals to", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"a","amount":1}`},
		{"missing amount", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b"}`},
		{"amount zero", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":0}`},
		{"amount negative", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":-5}`},
		{"amount float", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":10.5}`},
		{"amount float with zero fraction", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":10.0}`},
		{"amount exponent", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1e2}`},
		{"amount string", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":"100"}`},
		{"amount null", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":null}`},
		{"amount boolean", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":true}`},
		{"amount above maximum", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":100000001}`},
		{"amount beyond int64", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":9223372036854775808}`},
		{"unknown field", "", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1,"currency":"SGD"}`},
		{"malformed JSON", "", `{"idempotencyKey":"k",`},
		{"empty body", "", ``},
		{"whitespace body", "", "  \n "},
		{"JSON array", "", `[` + validBody + `]`},
		{"JSON null", "", `null`},
		{"JSON string", "", `"hello"`},
		{"two JSON objects", "", validBody + validBody},
		{"trailing garbage", "", validBody + ` x`},
		{"wrong Content-Type", "text/plain", validBody},
		{"form Content-Type", "application/x-www-form-urlencoded", validBody},
		{"missing Content-Type", "-", validBody},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newServer(t)
			ct := "application/json"
			switch tc.contentType {
			case "":
			case "-":
				ct = ""
			default:
				ct = tc.contentType
			}

			rec := s.do(http.MethodPost, "/transfers", ct, tc.body)

			assertError(t, rec, http.StatusBadRequest, "VALIDATION_ERROR")
			assert.Equal(t, 0, s.transfers.callCount(), "service must not be called")
		})
	}
}

func TestCreateTransfer_ValidationMessageNamesTheField(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	rec := s.postTransfer(`{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":10.5}`)
	b := assertError(t, rec, http.StatusBadRequest, "VALIDATION_ERROR")
	assert.Contains(t, b.Error.Message, "amount")
}

func TestCreateTransfer_AcceptsContentTypeWithCharset(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	rec := s.do(http.MethodPost, "/transfers", "application/json; charset=utf-8", validBody)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestCreateTransfer_BodyTooLarge(t *testing.T) {
	t.Parallel()
	big := `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1,"pad":"` + strings.Repeat("x", 2048) + `"}`

	t.Run("with Content-Length", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		rec := s.postTransfer(big)
		assertError(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
		assert.Equal(t, 0, s.transfers.callCount())
	})

	t.Run("without Content-Length (chunked)", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		req := httptest.NewRequest(http.MethodPost, "/transfers", io.MultiReader(strings.NewReader(big)))
		req.ContentLength = -1 // unknown length: the limit must be enforced while reading
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		assertError(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
		assert.Equal(t, 0, s.transfers.callCount())
	})
}

// ---- POST /transfers: success and outcomes --------------------------------

func TestCreateTransfer_PassesTheCommandToTheService(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.postTransfer(validBody)
	require.Equal(t, 1, s.transfers.callCount())
	assert.Equal(t, service.CreateTransferCommand{
		IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
	}, s.transfers.calls[0])
}

func TestCreateTransfer_Processed201(t *testing.T) {
	t.Parallel()
	s := newServer(t)

	rec := s.postTransfer(validBody)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Header().Get("Idempotent-Replayed"))
	assert.JSONEq(t, `{
		"id": "01920000-0000-7000-8000-000000000001",
		"idempotencyKey": "abc123",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 100,
		"status": "PROCESSED",
		"failureReason": null,
		"createdAt": "2026-09-26T10:00:00.123456Z"
	}`, rec.Body.String())
}

func TestCreateTransfer_Failed422(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.transfers.fn = func(context.Context, service.CreateTransferCommand) (service.TransferResult, error) {
		return service.TransferResult{Transfer: sampleTransfer(domain.StatusFailed, domain.ReasonInsufficientFunds)}, nil
	}

	rec := s.postTransfer(validBody)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "FAILED", body["status"])
	assert.Equal(t, "INSUFFICIENT_FUNDS", body["failureReason"])
}

func TestCreateTransfer_ReplayKeepsOriginalStatusAndSetsHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status     domain.Status
		reason     domain.FailureReason
		wantStatus int
	}{
		{domain.StatusProcessed, "", http.StatusCreated},
		{domain.StatusFailed, domain.ReasonCurrencyMismatch, http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()
			s := newServer(t)
			s.transfers.fn = func(context.Context, service.CreateTransferCommand) (service.TransferResult, error) {
				return service.TransferResult{Transfer: sampleTransfer(tc.status, tc.reason), Replayed: true}, nil
			}

			rec := s.postTransfer(validBody)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "true", rec.Header().Get("Idempotent-Replayed"))
		})
	}
}

func TestCreateTransfer_ErrorMapping(t *testing.T) {
	t.Parallel()
	const secret = `pq: relation "wallets" does not exist at 10.0.0.5:5432`

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation from service", &service.ValidationError{Field: "amount", Message: "must be at least 1"}, 400, "VALIDATION_ERROR"},
		{"bare validation sentinel", domain.ErrValidation, 400, "VALIDATION_ERROR"},
		{"wallet not found", fmt.Errorf("wallet ghost: %w", domain.ErrWalletNotFound), 404, "WALLET_NOT_FOUND"},
		{"key reused", domain.ErrIdempotencyKeyReused, 409, "IDEMPOTENCY_KEY_REUSED"},
		{"unavailable", fmt.Errorf("%w: %s", service.ErrUnavailable, secret), 503, "TEMPORARILY_UNAVAILABLE"},
		{"deadline exceeded", context.DeadlineExceeded, 503, "TEMPORARILY_UNAVAILABLE"},
		{"canceled", context.Canceled, 503, "TEMPORARILY_UNAVAILABLE"},
		{"unexpected", errors.New(secret), 500, "INTERNAL_ERROR"},
		{"invalid transition leaks as internal", fmt.Errorf("%w: %s", domain.ErrInvalidTransition, secret), 500, "INTERNAL_ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newServer(t)
			s.transfers.fn = func(context.Context, service.CreateTransferCommand) (service.TransferResult, error) {
				return service.TransferResult{}, tc.err
			}

			rec := s.postTransfer(validBody)

			assertError(t, rec, tc.wantStatus, tc.wantCode)
			assert.NotContains(t, rec.Body.String(), "pq:", "internal error text must never reach the client")
			assert.NotContains(t, rec.Body.String(), "10.0.0.5")
		})
	}
}

func TestCreateTransfer_UnexpectedPendingStatusIs500(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.transfers.fn = func(context.Context, service.CreateTransferCommand) (service.TransferResult, error) {
		return service.TransferResult{Transfer: sampleTransfer(domain.StatusPending, "")}, nil
	}
	assertError(t, s.postTransfer(validBody), http.StatusInternalServerError, "INTERNAL_ERROR")
}

// ---- middleware -----------------------------------------------------------

func TestRequestID(t *testing.T) {
	t.Parallel()

	t.Run("generated when absent", func(t *testing.T) {
		t.Parallel()
		rec := newServer(t).do(http.MethodGet, "/healthz", "", "")
		_, err := uuid.Parse(rec.Header().Get("X-Request-ID"))
		assert.NoError(t, err, "want a UUID, got %q", rec.Header().Get("X-Request-ID"))
	})

	t.Run("echoed when present", func(t *testing.T) {
		t.Parallel()
		rec := newServer(t).do(http.MethodGet, "/healthz", "", "", "X-Request-ID", "client-req-42")
		assert.Equal(t, "client-req-42", rec.Header().Get("X-Request-ID"))
	})

	t.Run("replaced when unsafe (log injection)", func(t *testing.T) {
		t.Parallel()
		rec := newServer(t).do(http.MethodGet, "/healthz", "", "", "X-Request-ID", "bad id\twith spaces")
		got := rec.Header().Get("X-Request-ID")
		assert.NotEqual(t, "bad id\twith spaces", got)
		_, err := uuid.Parse(got)
		assert.NoError(t, err)
	})

	t.Run("included in error bodies", func(t *testing.T) {
		t.Parallel()
		rec := newServer(t).do(http.MethodPost, "/transfers", "application/json", "{", "X-Request-ID", "trace-1")
		assert.Equal(t, "trace-1", decodeError(t, rec).Error.RequestID)
	})
}

func TestRecovery_PanicReturns500AndServerKeepsWorking(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	calls := 0
	s.transfers.fn = func(ctx context.Context, cmd service.CreateTransferCommand) (service.TransferResult, error) {
		calls++
		if calls == 1 {
			panic("database driver exploded: password=hunter2")
		}
		return processed(ctx, cmd)
	}

	rec := s.postTransfer(validBody)
	assertError(t, rec, http.StatusInternalServerError, "INTERNAL_ERROR")
	assert.NotContains(t, rec.Body.String(), "hunter2")

	rec = s.postTransfer(validBody)
	assert.Equal(t, http.StatusCreated, rec.Code, "the server must keep serving after a panic")
}

func TestTimeout_ServiceReceivesDeadlineAnd503OnExpiry(t *testing.T) {
	t.Parallel()
	s := newServer(t, func(c *handler.RouterConfig) { c.RequestTimeout = 50 * time.Millisecond })
	s.transfers.fn = func(ctx context.Context, cmd service.CreateTransferCommand) (service.TransferResult, error) {
		if _, ok := ctx.Deadline(); !ok {
			return service.TransferResult{}, errors.New("no deadline on context")
		}
		<-ctx.Done() // a slow database
		return service.TransferResult{}, fmt.Errorf("%w: %w", service.ErrUnavailable, ctx.Err())
	}

	start := time.Now()
	rec := s.postTransfer(validBody)

	assertError(t, rec, http.StatusServiceUnavailable, "TEMPORARILY_UNAVAILABLE")
	assert.Less(t, time.Since(start), time.Second)
}

func TestLogger_OneStructuredLinePerRequestWithoutBody(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	s.transfers.fn = func(context.Context, service.CreateTransferCommand) (service.TransferResult, error) {
		return service.TransferResult{Transfer: sampleTransfer(domain.StatusProcessed, ""), Replayed: true}, nil
	}

	s.do(http.MethodPost, "/transfers", "application/json", validBody, "X-Request-ID", "log-test-1")

	lines := strings.Split(strings.TrimSpace(s.logs.String()), "\n")
	require.Len(t, lines, 1, s.logs.String())
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &entry))
	assert.Equal(t, "log-test-1", entry["request_id"])
	assert.Equal(t, "POST", entry["method"])
	assert.Equal(t, "/transfers", entry["route"])
	assert.EqualValues(t, 201, entry["status"])
	assert.Contains(t, entry, "duration_ms")
	assert.Equal(t, "abc123", entry["idempotency_key"])
	assert.Equal(t, "01920000-0000-7000-8000-000000000001", entry["transfer_id"])
	assert.Equal(t, "replayed", entry["outcome"])
	assert.NotContains(t, lines[0], "wallet_2", "request bodies must not be logged")
}

func TestLogger_RecordsOutcomeForEachCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		res  service.TransferResult
		err  error
		want string
	}{
		{"processed", service.TransferResult{Transfer: sampleTransfer(domain.StatusProcessed, "")}, nil, "processed"},
		{"failed", service.TransferResult{Transfer: sampleTransfer(domain.StatusFailed, domain.ReasonInsufficientFunds)}, nil, "failed"},
		{"error", service.TransferResult{}, domain.ErrWalletNotFound, "error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newServer(t)
			s.transfers.fn = func(context.Context, service.CreateTransferCommand) (service.TransferResult, error) {
				return tc.res, tc.err
			}
			s.postTransfer(validBody)
			var entry map[string]any
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(s.logs.Bytes()), &entry))
			assert.Equal(t, tc.want, entry["outcome"])
		})
	}
}

func TestUnknownRouteReturnsJSON404(t *testing.T) {
	t.Parallel()
	assertError(t, newServer(t).do(http.MethodGet, "/nope", "", ""), http.StatusNotFound, "NOT_FOUND")
}

// ---- wallets --------------------------------------------------------------

func TestGetWallet(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		t.Parallel()
		rec := newServer(t).do(http.MethodGet, "/wallets/wallet_1", "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"id":"wallet_1","balance":500,"currency":"SGD","updatedAt":"2026-09-26T10:00:00.123456Z"}`, rec.Body.String())
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		s.wallets.getFn = func(context.Context, string) (*domain.Wallet, error) { return nil, domain.ErrWalletNotFound }
		assertError(t, s.do(http.MethodGet, "/wallets/ghost", "", ""), http.StatusNotFound, "WALLET_NOT_FOUND")
	})
}

func TestListWalletTransfers(t *testing.T) {
	t.Parallel()

	t.Run("default limit is 20 and empty list is []", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		rec := s.do(http.MethodGet, "/wallets/wallet_1/transfers", "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"transfers":[]}`, rec.Body.String())
		assert.Equal(t, []int{20}, s.wallets.gotList)
	})

	t.Run("explicit limit and transfer shape", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		s.wallets.listFn = func(context.Context, string, int) ([]domain.Transfer, error) {
			return []domain.Transfer{sampleTransfer(domain.StatusFailed, domain.ReasonInsufficientFunds)}, nil
		}
		rec := s.do(http.MethodGet, "/wallets/wallet_1/transfers?limit=5", "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, []int{5}, s.wallets.gotList)
		var body struct {
			Transfers []map[string]any `json:"transfers"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Transfers, 1)
		assert.Equal(t, "INSUFFICIENT_FUNDS", body.Transfers[0]["failureReason"])
	})

	// Not an integer: rejected while parsing, before the service is called.
	for _, limit := range []string{"abc", "1.5", ""} {
		t.Run("non-integer limit "+limit, func(t *testing.T) {
			t.Parallel()
			s := newServer(t)
			assertError(t, s.do(http.MethodGet, "/wallets/wallet_1/transfers?limit="+limit, "", ""),
				http.StatusBadRequest, "VALIDATION_ERROR")
			assert.Empty(t, s.wallets.gotList, "service must not be called")
		})
	}

	// An integer outside 1..100: the range is the service's rule. The handler
	// passes the value through, and maps the service's ValidationError to 400.
	for _, limit := range []int{0, 101, -1} {
		t.Run(fmt.Sprintf("out-of-range limit %d", limit), func(t *testing.T) {
			t.Parallel()
			s := newServer(t)
			s.wallets.listFn = func(context.Context, string, int) ([]domain.Transfer, error) {
				return nil, &service.ValidationError{Field: "limit", Message: "must be between 1 and 100"}
			}
			rec := s.do(http.MethodGet, fmt.Sprintf("/wallets/wallet_1/transfers?limit=%d", limit), "", "")
			b := assertError(t, rec, http.StatusBadRequest, "VALIDATION_ERROR")
			assert.Contains(t, b.Error.Message, "limit")
			assert.Equal(t, []int{limit}, s.wallets.gotList, "the value reaches the service unchanged")
		})
	}

	t.Run("wallet not found", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		s.wallets.listFn = func(context.Context, string, int) ([]domain.Transfer, error) { return nil, domain.ErrWalletNotFound }
		assertError(t, s.do(http.MethodGet, "/wallets/ghost/transfers", "", ""), http.StatusNotFound, "WALLET_NOT_FOUND")
	})
}

// ---- health ---------------------------------------------------------------

func TestHealth(t *testing.T) {
	t.Parallel()

	t.Run("healthz does not touch the database", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		s.pinger.err = errors.New("db down")
		assert.Equal(t, http.StatusOK, s.do(http.MethodGet, "/healthz", "", "").Code)
	})

	t.Run("readyz ok", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, http.StatusOK, newServer(t).do(http.MethodGet, "/readyz", "", "").Code)
	})

	t.Run("readyz 503 when the database is down", func(t *testing.T) {
		t.Parallel()
		s := newServer(t)
		s.pinger.err = errors.New("dial tcp 10.0.0.5:5432: connection refused")
		rec := s.do(http.MethodGet, "/readyz", "", "")
		assertError(t, rec, http.StatusServiceUnavailable, "TEMPORARILY_UNAVAILABLE")
		assert.NotContains(t, rec.Body.String(), "10.0.0.5")
	})
}
