package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/app"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil/pgtest"
)

// End-to-end: real HTTP server, real wiring, real Postgres.
func TestEndToEnd_TransferLifecycleOverHTTP(t *testing.T) {
	pool := pgtest.NewDB(t)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ('wallet_1', 100), ('wallet_2', 0)`)
	require.NoError(t, err)
	srv := httptest.NewServer(app.NewHandler(pool, 3*time.Second, pgtest.Logger()))
	t.Cleanup(srv.Close)

	type response struct {
		status int
		header http.Header
		body   map[string]any
	}
	post := func(body string) response {
		t.Helper()
		resp, err := http.Post(srv.URL+"/transfers", "application/json", strings.NewReader(body))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return response{status: resp.StatusCode, header: resp.Header, body: out}
	}
	const body = `{"idempotencyKey":"e2e-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":60}`

	// 1. Original request executes.
	first := post(body)
	require.Equal(t, http.StatusCreated, first.status)
	require.Equal(t, "PROCESSED", first.body["status"])
	require.Empty(t, first.header.Get("Idempotent-Replayed"))
	created := first.body

	// 2. Retry with the same key replays the identical result.
	replay := post(body)
	require.Equal(t, http.StatusCreated, replay.status)
	require.Equal(t, "true", replay.header.Get("Idempotent-Replayed"))
	require.Equal(t, created, replay.body)

	// 3. Not enough left for a second 60: recorded as FAILED.
	failed := post(`{"idempotencyKey":"e2e-2","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":60}`)
	require.Equal(t, http.StatusUnprocessableEntity, failed.status)
	require.Equal(t, "INSUFFICIENT_FUNDS", failed.body["error"].(map[string]any)["code"])

	// 4. Same key, different payload.
	reused := post(`{"idempotencyKey":"e2e-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1}`)
	require.Equal(t, http.StatusUnprocessableEntity, reused.status)
	require.Equal(t, "IDEMPOTENCY_KEY_REUSED", reused.body["error"].(map[string]any)["code"])

	// 5. Balances and the transfer are visible through the read API.
	get := func(path string) (int, map[string]any) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return resp.StatusCode, out
	}
	status, w1 := get("/wallets/wallet_1")
	require.Equal(t, http.StatusOK, status)
	require.InDelta(t, 40, w1["balance"], 0)
	_, w2 := get("/wallets/wallet_2")
	require.InDelta(t, 60, w2["balance"], 0)
	status, fetched := get("/transfers/" + created["id"].(string))
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, created, fetched)

	// 6. Readiness reflects the database.
	ready, err := http.Get(srv.URL + "/readyz")
	require.NoError(t, err)
	require.NoError(t, ready.Body.Close())
	require.Equal(t, http.StatusOK, ready.StatusCode)
}
