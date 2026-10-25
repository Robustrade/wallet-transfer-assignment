package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/handler"
)

// The API is exercised over real HTTP against real PostgreSQL: this is the
// "exactly-once semantics at the API level" contract end to end.

type apiResponse struct {
	Status int
	Header http.Header
	Body   map[string]any
}

func newAPI(t *testing.T) (*env, *httptest.Server) {
	t.Helper()
	e := newEnv(t)
	srv := httptest.NewServer(handler.New(e.transfers, e.wallets, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	return e, srv
}

func call(t *testing.T, method, url, body string) apiResponse {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	out := apiResponse{Status: resp.StatusCode, Header: resp.Header}
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &out.Body), string(raw))
	return out
}

func transferJSON(key, from, to string, amount int) string {
	return fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":%d}`, key, from, to, amount)
}

func TestAPI_DuplicateRequestReturnsOriginalResultWithoutDuplicateTransfer(t *testing.T) {
	e, srv := newAPI(t)
	e.wallet(t, "wallet_1", 500)
	e.wallet(t, "wallet_2", 0)
	body := transferJSON("abc123", "wallet_1", "wallet_2", 100)

	first := call(t, http.MethodPost, srv.URL+"/transfers", body)
	second := call(t, http.MethodPost, srv.URL+"/transfers", body)

	assert.Equal(t, http.StatusCreated, first.Status)
	assert.Equal(t, http.StatusOK, second.Status)
	assert.Equal(t, "true", second.Header.Get(handler.ReplayedHeader))
	assert.Equal(t, first.Body, second.Body, "a replay returns the complete original response, timestamps included")
	assert.Equal(t, "PROCESSED", second.Body["status"])
	assert.EqualValues(t, 400, e.balance(t, "wallet_1"))
	assert.EqualValues(t, 100, e.balance(t, "wallet_2"))
	assert.Equal(t, 1, e.count(t, "transfers"))
	assert.Equal(t, 2, e.count(t, "ledger_entries"))
}

func TestAPI_FailureIsReplayedWithTheSameStatus(t *testing.T) {
	e, srv := newAPI(t)
	e.wallet(t, "a", 10)
	e.wallet(t, "b", 0)
	body := transferJSON("k", "a", "b", 100)

	first := call(t, http.MethodPost, srv.URL+"/transfers", body)
	second := call(t, http.MethodPost, srv.URL+"/transfers", body)

	assert.Equal(t, http.StatusUnprocessableEntity, first.Status)
	assert.Equal(t, http.StatusUnprocessableEntity, second.Status)
	assert.Equal(t, "FAILED", second.Body["status"])
	assert.Equal(t, first.Body, second.Body, "a replayed failure is byte-for-byte the original")
	assert.EqualValues(t, 10, e.balance(t, "a"))
}

// JSON can smuggle invalid text in as escapes: a lone surrogate such as \ud800
// decodes to U+FFFD, so different escapes would otherwise share one identity.
func TestAPI_MalformedTextCannotCollapseIntoOneIdentity(t *testing.T) {
	e, srv := newAPI(t)
	e.wallet(t, "a", 100)
	e.wallet(t, "b", 0)

	cases := map[string]string{
		"lone high surrogate in key":   `{"idempotencyKey":"k\ud800","fromWalletId":"a","toWalletId":"b","amount":1}`,
		"another lone surrogate":       `{"idempotencyKey":"k\ud801","fromWalletId":"a","toWalletId":"b","amount":1}`,
		"lone surrogate in wallet id":  `{"idempotencyKey":"k","fromWalletId":"a\udc00","toWalletId":"b","amount":1}`,
		"literal replacement in key":   "{\"idempotencyKey\":\"k�\",\"fromWalletId\":\"a\",\"toWalletId\":\"b\",\"amount\":1}",
		"invalid raw bytes in key":     "{\"idempotencyKey\":\"k\xff\",\"fromWalletId\":\"a\",\"toWalletId\":\"b\",\"amount\":1}",
		"other invalid raw bytes":      "{\"idempotencyKey\":\"k\xfe\",\"fromWalletId\":\"a\",\"toWalletId\":\"b\",\"amount\":1}",
		"lone surrogate in new wallet": `{"id":"w\ud800","balance":1}`,
	}
	for name, body := range cases {
		path := "/transfers"
		if name == "lone surrogate in new wallet" {
			path = "/wallets"
		}
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusBadRequest, call(t, http.MethodPost, srv.URL+path, body).Status)
		})
	}
	assert.Equal(t, 0, e.count(t, "transfers"))
	assert.Equal(t, 0, e.count(t, "idempotency_records"))
	assert.Equal(t, 2, e.count(t, "wallets"), "no wallet was created from malformed text")
}

func TestAPI_DestinationOverflowIsAReplayable422(t *testing.T) {
	e, srv := newAPI(t)
	e.wallet(t, "src", 100)
	e.wallet(t, "full", math.MaxInt64)
	body := transferJSON("k", "src", "full", 1)

	first := call(t, http.MethodPost, srv.URL+"/transfers", body)
	second := call(t, http.MethodPost, srv.URL+"/transfers", body)

	assert.Equal(t, http.StatusUnprocessableEntity, first.Status)
	assert.Equal(t, "BALANCE_OVERFLOW", first.Body["failureReason"])
	assert.Equal(t, "balance_overflow", first.Body["error"].(map[string]any)["code"])
	assert.Equal(t, http.StatusUnprocessableEntity, second.Status)
	assert.Equal(t, "true", second.Header.Get(handler.ReplayedHeader))
	assert.Equal(t, first.Body, second.Body)
	assert.EqualValues(t, 100, e.balance(t, "src"))
}

func TestAPI_ReusingAKeyWithADifferentPayloadIsRejected(t *testing.T) {
	e, srv := newAPI(t)
	e.wallet(t, "a", 500)
	e.wallet(t, "b", 0)
	call(t, http.MethodPost, srv.URL+"/transfers", transferJSON("k", "a", "b", 100))

	resp := call(t, http.MethodPost, srv.URL+"/transfers", transferJSON("k", "a", "b", 200))

	assert.Equal(t, http.StatusUnprocessableEntity, resp.Status)
	assert.EqualValues(t, 400, e.balance(t, "a"))
}

func TestAPI_ConcurrentDuplicatesExecuteOnce(t *testing.T) {
	e, srv := newAPI(t)
	e.wallet(t, "a", 1000)
	e.wallet(t, "b", 0)
	body := transferJSON("same", "a", "b", 100)

	const n = 20
	statuses := make([]int, n)
	runConcurrently(t, n, time.Minute, func(ctx context.Context, i int) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/transfers", bytes.NewBufferString(body))
		if !assert.NoError(t, err) {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if assert.NoError(t, err) {
			statuses[i] = resp.StatusCode
			_ = resp.Body.Close()
		}
	})

	created := 0
	for _, s := range statuses {
		if s == http.StatusCreated {
			created++
		} else {
			assert.Equal(t, http.StatusOK, s)
		}
	}
	assert.Equal(t, 1, created, "exactly one request executed the transfer")
	assert.EqualValues(t, 900, e.balance(t, "a"))
	assert.Equal(t, 1, e.count(t, "transfers"))
	e.assertLedgerReconciles(t)
}

func TestAPI_WalletsAndTransferLookup(t *testing.T) {
	_, srv := newAPI(t)
	assert.Equal(t, http.StatusCreated, call(t, http.MethodPost, srv.URL+"/wallets", `{"id":"a","balance":300}`).Status)
	assert.Equal(t, http.StatusCreated, call(t, http.MethodPost, srv.URL+"/wallets", `{"id":"b","balance":0}`).Status)
	assert.Equal(t, http.StatusConflict, call(t, http.MethodPost, srv.URL+"/wallets", `{"id":"a","balance":1}`).Status)

	created := call(t, http.MethodPost, srv.URL+"/transfers", transferJSON("k", "a", "b", 120))
	require.Equal(t, http.StatusCreated, created.Status)

	wallet := call(t, http.MethodGet, srv.URL+"/wallets/a", "")
	assert.EqualValues(t, 180, wallet.Body["balance"])

	got := call(t, http.MethodGet, srv.URL+"/transfers/"+created.Body["id"].(string), "")
	require.Equal(t, http.StatusOK, got.Status)
	entries, ok := got.Body["ledgerEntries"].([]any)
	require.True(t, ok)
	assert.Len(t, entries, 2)

	assert.Equal(t, http.StatusNotFound, call(t, http.MethodPost, srv.URL+"/transfers", transferJSON("k2", "a", "ghost", 1)).Status)
}

// PostgreSQL TEXT cannot hold NUL or invalid UTF-8. Such input must be a 400
// from validation, never a 500 from the database.
func TestAPI_RejectsTextThatPostgresCannotStore(t *testing.T) {
	_, srv := newAPI(t)
	require.Equal(t, http.StatusCreated, call(t, http.MethodPost, srv.URL+"/wallets", `{"id":"a","balance":10}`).Status)

	transfers := map[string]string{
		"NUL in key":         `{"idempotencyKey":"k\u0000","fromWalletId":"a","toWalletId":"b","amount":1}`,
		"NUL in source":      `{"idempotencyKey":"k","fromWalletId":"a\u0000","toWalletId":"b","amount":1}`,
		"NUL in destination": `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"\u0000b","amount":1}`,
	}
	for name, body := range transfers {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusBadRequest, call(t, http.MethodPost, srv.URL+"/transfers", body).Status)
		})
	}

	t.Run("NUL in new wallet id", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, call(t, http.MethodPost, srv.URL+"/wallets", `{"id":"x\u0000y","balance":1}`).Status)
	})
	t.Run("NUL in wallet path", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, call(t, http.MethodGet, srv.URL+"/wallets/a%00b", "").Status)
	})
	t.Run("invalid UTF-8 in wallet path", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, call(t, http.MethodGet, srv.URL+"/wallets/a%ff", "").Status)
	})
}
