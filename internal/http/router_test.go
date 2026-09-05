package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/domain"
	apphttp "wallet-transfer/internal/http"
	"wallet-transfer/internal/http/handlers"
	"wallet-transfer/internal/metrics"
	"wallet-transfer/internal/service"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

var errUnexpected = errors.New("database connection reset by peer")

type stubTransferService struct {
	outcome service.TransferOutcome
	detail  service.TransferDetail
	err     error
	panic   bool
	delay   time.Duration
	seen    domain.TransferRequest
}

func (s *stubTransferService) Transfer(ctx context.Context, req domain.TransferRequest) (service.TransferOutcome, error) {
	s.seen = req
	if s.panic {
		panic("boom")
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return service.TransferOutcome{}, ctx.Err()
		}
	}
	return s.outcome, s.err
}

func (s *stubTransferService) Get(context.Context, uuid.UUID) (service.TransferDetail, error) {
	return s.detail, s.err
}

type stubWalletService struct {
	wallet    domain.Wallet
	transfers []domain.Transfer
	err       error
}

func (s *stubWalletService) Get(context.Context, string) (domain.Wallet, error) {
	return s.wallet, s.err
}

func (s *stubWalletService) History(context.Context, string, int) ([]domain.Transfer, error) {
	return s.transfers, s.err
}

type stubHealth struct{ err error }

func (s stubHealth) Ping(context.Context) error { return s.err }

type testServer struct {
	engine    *gin.Engine
	transfers *stubTransferService
	wallets   *stubWalletService
	health    *stubHealth
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	srv := &testServer{
		transfers: &stubTransferService{outcome: service.TransferOutcome{Result: sampleResult()}},
		wallets:   &stubWalletService{wallet: sampleWallet()},
		health:    &stubHealth{},
	}

	registry := prometheus.NewRegistry()
	srv.engine = apphttp.NewRouter(apphttp.RouterConfig{
		Transfers:      srv.transfers,
		Wallets:        srv.wallets,
		Health:         srv.health,
		Metrics:        metrics.New(registry),
		Registry:       registry,
		RequestTimeout: time.Second,
	})
	return srv
}

func (s *testServer) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	switch payload := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(payload))
	default:
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	s.engine.ServeHTTP(recorder, req)
	return recorder
}

func sampleResult() service.TransferResult {
	return service.TransferResult{
		TransferID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Status:       domain.StatusProcessed,
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       2500,
		Currency:     "INR",
		CreatedAt:    time.Unix(1700000000, 0).UTC(),
	}
}

func sampleWallet() domain.Wallet {
	return domain.Wallet{ID: "wallet_1", Currency: "INR", Balance: 97500, UpdatedAt: time.Unix(1700000000, 0).UTC()}
}

func validBody() map[string]any {
	return map[string]any{
		"idempotencyKey": "key-1",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         2500,
	}
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) handlers.ErrorBody {
	t.Helper()

	var body handlers.ErrorBody
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func TestCreateTransfer(t *testing.T) {
	t.Parallel()

	t.Run("returns 201 and the created transfer", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, http.StatusCreated, recorder.Code)
		require.Equal(t, "false", recorder.Header().Get("Idempotent-Replay"))

		var body map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		require.Equal(t, "11111111-1111-1111-1111-111111111111", body["transferId"])
		require.Equal(t, "PROCESSED", body["status"])
		require.EqualValues(t, 2500, body["amount"])
	})

	t.Run("returns 200 and flags a replay", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.transfers.outcome.Replayed = true

		recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, http.StatusOK, recorder.Code, "a replay is a success, not a conflict")
		require.Equal(t, "true", recorder.Header().Get("Idempotent-Replay"))
	})

	t.Run("passes the request through to the service unchanged", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, "key-1", srv.transfers.seen.IdempotencyKey)
		require.Equal(t, "wallet_1", srv.transfers.seen.FromWalletID)
		require.Equal(t, "wallet_2", srv.transfers.seen.ToWalletID)
		require.EqualValues(t, 2500, srv.transfers.seen.Amount)
	})

	t.Run("maps every domain error onto its status code", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			err      error
			expected int
			code     handlers.ErrorCode
		}{
			{"validation error", domain.ErrValidation, http.StatusBadRequest, handlers.CodeValidation},
			{"wallet not found", domain.ErrWalletNotFound, http.StatusNotFound, handlers.CodeWalletNotFound},
			{"insufficient funds", domain.ErrInsufficientFunds, http.StatusUnprocessableEntity, handlers.CodeInsufficient},
			{"currency mismatch", domain.ErrCurrencyMismatch, http.StatusUnprocessableEntity, handlers.CodeCurrencyMismatch},
			{"idempotency payload conflict", domain.ErrIdempotencyKeyConflict, http.StatusConflict, handlers.CodeKeyConflict},
			{"database conflict", domain.ErrConflict, http.StatusConflict, handlers.CodeRetryableConflict},
			{"database timeout", domain.ErrTimeout, http.StatusGatewayTimeout, handlers.CodeTimeout},
			{"unexpected error", errUnexpected, http.StatusInternalServerError, handlers.CodeInternal},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				srv := newTestServer(t)
				srv.transfers.err = tc.err

				recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

				require.Equal(t, tc.expected, recorder.Code)
				require.Equal(t, tc.code, decodeError(t, recorder).Error.Code)
			})
		}
	})

	t.Run("an internal error never leaks its detail to the client", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.transfers.err = errUnexpected

		recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, http.StatusInternalServerError, recorder.Code)
		require.Equal(t, "an unexpected error occurred", decodeError(t, recorder).Error.Message)
	})

	t.Run("rejects a malformed body", func(t *testing.T) {
		t.Parallel()

		cases := map[string]string{
			"invalid json":               `{"amount": 100,`,
			"empty body":                 ``,
			"missing amount":             `{"fromWalletId":"w1","toWalletId":"w2","idempotencyKey":"k"}`,
			"missing idempotency key":    `{"fromWalletId":"w1","toWalletId":"w2","amount":100}`,
			"amount as string":           `{"fromWalletId":"w1","toWalletId":"w2","idempotencyKey":"k","amount":"100"}`,
			"missing source wallet":      `{"toWalletId":"w2","idempotencyKey":"k","amount":100}`,
			"missing destination wallet": `{"fromWalletId":"w1","idempotencyKey":"k","amount":100}`,
		}

		for name, payload := range cases {
			t.Run(name, func(t *testing.T) {
				srv := newTestServer(t)
				recorder := srv.do(t, http.MethodPost, "/transfers", payload)

				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Equal(t, handlers.CodeValidation, decodeError(t, recorder).Error.Code)
			})
		}
	})

	t.Run("an explicit zero amount reaches the service rather than looking absent", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		body := validBody()
		body["amount"] = 0

		srv.do(t, http.MethodPost, "/transfers", body)

		require.EqualValues(t, 0, srv.transfers.seen.Amount,
			"pointer-to-int unmarshaling must preserve 0 so domain validation can reject it")
	})
}

func TestGetTransfer(t *testing.T) {
	t.Parallel()

	t.Run("returns the transfer with both ledger entries", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		targetID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		now := time.Now().UTC().Truncate(time.Second)

		srv.transfers.detail = service.TransferDetail{
			Transfer: domain.Transfer{
				ID:           targetID,
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       1500,
				Currency:     "INR",
				Status:       domain.StatusProcessed,
				CreatedAt:    now,
			},
			Entries: []domain.LedgerEntry{
				{ID: 1, TransferID: targetID, WalletID: "wallet_1", Type: domain.EntryTypeDebit, Amount: 1500, Currency: "INR", CreatedAt: now},
				{ID: 2, TransferID: targetID, WalletID: "wallet_2", Type: domain.EntryTypeCredit, Amount: 1500, Currency: "INR", CreatedAt: now},
			},
		}

		recorder := srv.do(t, http.MethodGet, "/transfers/"+targetID.String(), nil)

		require.Equal(t, http.StatusOK, recorder.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		require.Equal(t, targetID.String(), body["transferId"])
		require.Equal(t, "PROCESSED", body["status"])
		entries, ok := body["ledgerEntries"].([]any)
		require.True(t, ok)
		require.Len(t, entries, 2)
	})

	t.Run("rejects an id that is not a UUID", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		recorder := srv.do(t, http.MethodGet, "/transfers/not-a-uuid", nil)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Equal(t, handlers.CodeValidation, decodeError(t, recorder).Error.Code)
	})

	t.Run("reports an unknown transfer as 404", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.transfers.err = domain.ErrTransferNotFound

		recorder := srv.do(t, http.MethodGet, "/transfers/"+uuid.NewString(), nil)

		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Equal(t, handlers.CodeTransferNotFound, decodeError(t, recorder).Error.Code)
	})
}

func TestWalletEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("returns a balance", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		recorder := srv.do(t, http.MethodGet, "/wallets/wallet_1", nil)

		require.Equal(t, http.StatusOK, recorder.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		require.Equal(t, "wallet_1", body["walletId"])
		require.Equal(t, "INR", body["currency"])
		require.EqualValues(t, 97500, body["balance"])
	})

	t.Run("reports an unknown wallet as 404", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.wallets.err = domain.ErrWalletNotFound

		recorder := srv.do(t, http.MethodGet, "/wallets/nonexistent", nil)

		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Equal(t, handlers.CodeWalletNotFound, decodeError(t, recorder).Error.Code)
	})

	t.Run("returns history", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.wallets.transfers = []domain.Transfer{
			{
				ID:           uuid.New(),
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       1000,
				Currency:     "INR",
				Status:       domain.StatusProcessed,
				CreatedAt:    time.Now().UTC(),
			},
		}

		recorder := srv.do(t, http.MethodGet, "/wallets/wallet_1/transfers?limit=5", nil)

		require.Equal(t, http.StatusOK, recorder.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		transfers, ok := body["transfers"].([]any)
		require.True(t, ok)
		require.Len(t, transfers, 1)
	})

	t.Run("rejects a non-numeric limit", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		recorder := srv.do(t, http.MethodGet, "/wallets/wallet_1/transfers?limit=abc", nil)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Equal(t, handlers.CodeValidation, decodeError(t, recorder).Error.Code)
	})
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("echoes a supplied request id", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("X-Request-ID", "caller-supplied-id")
		recorder := httptest.NewRecorder()
		srv.engine.ServeHTTP(recorder, req)

		require.Equal(t, "caller-supplied-id", recorder.Header().Get("X-Request-ID"))
	})

	t.Run("generates a request id when none is supplied", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		recorder := srv.do(t, http.MethodGet, "/healthz", nil)

		_, err := uuid.Parse(recorder.Header().Get("X-Request-ID"))
		require.NoError(t, err, "a generated request id must be a UUID")
	})

	t.Run("recovers from a panic as a 500 rather than crashing", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.transfers.panic = true

		recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, http.StatusInternalServerError, recorder.Code)
		require.Equal(t, handlers.CodeInternal, decodeError(t, recorder).Error.Code)
	})

	t.Run("times a slow request out instead of hanging", func(t *testing.T) {
		t.Parallel()

		srv := newTestServer(t)
		registry := prometheus.NewRegistry()
		srv.transfers.delay = time.Second
		srv.engine = apphttp.NewRouter(apphttp.RouterConfig{
			Transfers:      srv.transfers,
			Wallets:        srv.wallets,
			Health:         srv.health,
			Metrics:        metrics.New(registry),
			Registry:       registry,
			RequestTimeout: 10 * time.Millisecond,
		})

		recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
		require.Equal(t, handlers.CodeTimeout, decodeError(t, recorder).Error.Code)
	})

	t.Run("returns a structured 404 for an unknown route", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		recorder := srv.do(t, http.MethodGet, "/nope", nil)

		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Equal(t, "NOT_FOUND", string(decodeError(t, recorder).Error.Code))
	})
}

func TestOperationalEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("liveness does not depend on the database", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.health.err = errUnexpected

		require.Equal(t, http.StatusOK, srv.do(t, http.MethodGet, "/healthz", nil).Code)
	})

	t.Run("readiness fails when the database is unreachable", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.health.err = errUnexpected

		require.Equal(t, http.StatusServiceUnavailable, srv.do(t, http.MethodGet, "/readyz", nil).Code)
	})

	t.Run("readiness succeeds when the database answers", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		require.Equal(t, http.StatusOK, srv.do(t, http.MethodGet, "/readyz", nil).Code)
	})

	t.Run("metrics record transfer outcomes", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)

		srv.do(t, http.MethodPost, "/transfers", validBody())

		body := srv.do(t, http.MethodGet, "/metrics", nil).Body.String()
		require.Contains(t, body, `transfers_total{outcome="created"} 1`)
		require.Contains(t, body, `http_requests_total{method="POST",route="/transfers",status="201"} 1`)
	})
}

func TestRetryableConflictsAreReportedAsSuch(t *testing.T) {
	t.Parallel()

	t.Run("transient contention is a retryable 409", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.transfers.err = domain.ErrConflict

		recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, http.StatusConflict, recorder.Code)

		body := decodeError(t, recorder)
		require.Equal(t, handlers.CodeRetryableConflict, body.Error.Code)
		require.Contains(t, body.Error.Message, "retry",
			"the client must be told the same request is safe to send again")
	})

	t.Run("a database timeout is a 504, not a 500", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t)
		srv.transfers.err = domain.ErrTimeout

		recorder := srv.do(t, http.MethodPost, "/transfers", validBody())

		require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
		require.Equal(t, handlers.CodeTimeout, decodeError(t, recorder).Error.Code)
	})
}
