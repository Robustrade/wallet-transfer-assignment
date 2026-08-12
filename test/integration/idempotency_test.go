package integration_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdempotency_SequentialReplay(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_1", "INR")
	createWallet(t, srv, "wallet_2", "INR")
	fundWallet(t, srv, "wallet_1", 1000)

	req := map[string]any{"idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100}

	first := postJSON(t, srv.URL+"/transfers", req)
	firstBody := decodeJSON(t, first)
	require.Equal(t, http.StatusCreated, first.StatusCode)
	require.Empty(t, first.Header.Get("Idempotent-Replay"))

	second := postJSON(t, srv.URL+"/transfers", req)
	secondBody := decodeJSON(t, second)
	require.Equal(t, http.StatusCreated, second.StatusCode)
	require.Equal(t, "true", second.Header.Get("Idempotent-Replay"))
	require.Equal(t, firstBody, secondBody, "a replay must return the byte-identical body")

	require.Equal(t, int64(900), getWalletBalance(t, db, "wallet_1"), "balance must move exactly once")
	require.Equal(t, int64(100), getWalletBalance(t, db, "wallet_2"))

	transferID, _ := firstBody["id"].(string)
	debits, credits := countLedgerEntries(t, db, transferID)
	require.Equal(t, 1, debits)
	require.Equal(t, 1, credits)

	var transferRowCount int
	require.NoError(t, db.Gorm().WithContext(t.Context()).
		Raw(`SELECT COUNT(*) FROM transfers WHERE idempotency_key = 'abc123'`).Scan(&transferRowCount).Error)
	require.Equal(t, 1, transferRowCount, "exactly one transfer row for the key")

	assertInvariants(t, db)
}

func TestIdempotency_KeyReuseWithDifferentPayloadIsRejected(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_1", "INR")
	createWallet(t, srv, "wallet_2", "INR")
	fundWallet(t, srv, "wallet_1", 1000)

	first := postJSON(t, srv.URL+"/transfers", map[string]any{"idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100})
	firstBody := decodeJSON(t, first)
	require.Equal(t, http.StatusCreated, first.StatusCode)

	second := postJSON(t, srv.URL+"/transfers", map[string]any{"idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 999})
	require.Equal(t, http.StatusConflict, second.StatusCode)
	secondBody := decodeJSON(t, second)
	errBody := secondBody["error"].(map[string]any)
	require.Equal(t, "IDEMPOTENCY_KEY_REUSE", errBody["code"])

	// The original transfer must be completely untouched.
	verify := postJSON(t, srv.URL+"/transfers", map[string]any{"idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100})
	require.Equal(t, firstBody, decodeJSON(t, verify))

	assertInvariants(t, db)
}

// TestIdempotency_ConcurrentDuplicateRequests fires the same request many
// times simultaneously and asserts Postgres's INSERT ... ON CONFLICT
// blocking behaviour (docs/design.md §6) deduplicates them into exactly one
// transfer with exactly one non-replayed response, no matter how the
// goroutines interleave.
func TestIdempotency_ConcurrentDuplicateRequests(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_1", "INR")
	createWallet(t, srv, "wallet_2", "INR")
	fundWallet(t, srv, "wallet_1", 1000)

	const concurrency = 25
	req := map[string]any{"idempotencyKey": "concurrent-key", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100}

	var wg sync.WaitGroup
	bodies := make([]map[string]any, concurrency)
	replayHeaders := make([]string, concurrency)
	statuses := make([]int, concurrency)

	start := make(chan struct{})
	for i := range concurrency {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp := postJSON(t, srv.URL+"/transfers", req)
			statuses[i] = resp.StatusCode
			replayHeaders[i] = resp.Header.Get("Idempotent-Replay")
			bodies[i] = decodeJSON(t, resp)
		}(i)
	}
	close(start)
	wg.Wait()

	nonReplayed := 0
	for i := range concurrency {
		require.Equal(t, http.StatusCreated, statuses[i], "every attempt must settle successfully, replay or not")
		require.Equal(t, bodies[0], bodies[i], "every response body must be identical")
		if replayHeaders[i] == "" {
			nonReplayed++
		}
	}
	require.Equal(t, 1, nonReplayed, "exactly one of the concurrent attempts must be the original, non-replayed request")

	var transferRowCount int
	require.NoError(t, db.Gorm().WithContext(t.Context()).
		Raw(`SELECT COUNT(*) FROM transfers WHERE idempotency_key = 'concurrent-key'`).Scan(&transferRowCount).Error)
	require.Equal(t, 1, transferRowCount, "exactly one transfer row despite 25 concurrent requests")

	transferID, _ := bodies[0]["id"].(string)
	debits, credits := countLedgerEntries(t, db, transferID)
	require.Equal(t, 1, debits)
	require.Equal(t, 1, credits)

	require.Equal(t, int64(900), getWalletBalance(t, db, "wallet_1"), "balance must move exactly once despite 25 concurrent requests")
	require.Equal(t, int64(100), getWalletBalance(t, db, "wallet_2"))

	assertInvariants(t, db)
}
