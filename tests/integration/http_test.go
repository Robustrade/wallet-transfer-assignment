package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/handler"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// newRouter wires the real handler stack to store, as main.go does.
func newRouter(store *repository.Store) http.Handler {
	return handler.NewRouter(handler.RouterConfig{
		Transfers:      service.NewTransferService(store.TxManager),
		Wallets:        service.NewWalletService(store.Repos),
		DB:             store.DB,
		Logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		RequestTimeout: 5 * time.Second,
		MaxBodyBytes:   1 << 20,
	})
}

type httpResult struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func call(t *testing.T, h http.Handler, method, path, body string) httpResult {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := httpResult{status: rec.Code, header: rec.Header(), raw: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &res.body)
	return res
}

func TestHTTP_EndToEnd(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("wallet_1", 1000, "SGD")
		e.seedWallet("wallet_2", 0, "SGD")
		e.seedWallet("wallet_usd", 0, "USD")
		h := newRouter(e.store)
		post := func(body string) httpResult { return call(t, h, http.MethodPost, "/transfers", body) }

		first := post(`{"idempotencyKey":"e2e-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":300}`)
		require.Equal(t, http.StatusCreated, first.status, first.raw)
		assert.Equal(t, "PROCESSED", first.body["status"])
		assert.Nil(t, first.body["failureReason"])
		assert.NotEmpty(t, first.header.Get("X-Request-ID"))

		replay := post(`{"idempotencyKey":"e2e-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":300}`)
		assert.Equal(t, http.StatusCreated, replay.status)
		assert.Equal(t, "true", replay.header.Get("Idempotent-Replayed"))
		assert.Equal(t, first.raw, replay.raw, "a replay returns a byte-identical body")

		conflict := post(`{"idempotencyKey":"e2e-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":301}`)
		assert.Equal(t, http.StatusConflict, conflict.status)

		failed := post(`{"idempotencyKey":"e2e-2","fromWalletId":"wallet_2","toWalletId":"wallet_1","amount":99999}`)
		assert.Equal(t, http.StatusUnprocessableEntity, failed.status)
		assert.Equal(t, "INSUFFICIENT_FUNDS", failed.body["failureReason"])

		mismatch := post(`{"idempotencyKey":"e2e-3","fromWalletId":"wallet_1","toWalletId":"wallet_usd","amount":1}`)
		assert.Equal(t, http.StatusUnprocessableEntity, mismatch.status)
		assert.Equal(t, "CURRENCY_MISMATCH", mismatch.body["failureReason"])

		missing := post(`{"idempotencyKey":"e2e-4","fromWalletId":"wallet_1","toWalletId":"nobody","amount":1}`)
		assert.Equal(t, http.StatusNotFound, missing.status)

		wallet := call(t, h, http.MethodGet, "/wallets/wallet_1", "")
		require.Equal(t, http.StatusOK, wallet.status)
		assert.EqualValues(t, 700, wallet.body["balance"])

		history := call(t, h, http.MethodGet, "/wallets/wallet_1/transfers?limit=10", "")
		require.Equal(t, http.StatusOK, history.status)
		assert.Len(t, history.body["transfers"], 3, "PROCESSED, FAILED and CURRENCY_MISMATCH involve wallet_1")

		assert.Equal(t, http.StatusOK, call(t, h, http.MethodGet, "/readyz", "").status)
	})
}

func TestHTTP_DatabaseClosed(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100, "SGD")
		e.seedWallet("b", 0, "SGD")

		// A second store on the same database, which we close. The test
		// env's own store stays open for the invariant checker.
		store, err := repository.Open(context.Background(), repository.Options{
			Driver: driver, DSN: e.dsn, LockTimeout: time.Second,
		})
		require.NoError(t, err)
		h := newRouter(store)
		require.NoError(t, store.Close())

		ready := call(t, h, http.MethodGet, "/readyz", "")
		assert.Equal(t, http.StatusServiceUnavailable, ready.status)
		assert.Equal(t, "TEMPORARILY_UNAVAILABLE", ready.body["error"].(map[string]any)["code"])

		res := call(t, h, http.MethodPost, "/transfers",
			`{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1}`)
		assert.Contains(t, []int{http.StatusInternalServerError, http.StatusServiceUnavailable}, res.status)
		require.NotNil(t, res.body["error"], res.raw)
		assert.NotContains(t, res.raw, "sql:", "driver error text must not leak")
		assert.NotContains(t, res.raw, "closed")

		assert.Equal(t, http.StatusOK, call(t, h, http.MethodGet, "/healthz", "").status,
			"liveness does not depend on the database")
		e.assertNothingPersisted()
	})
}
