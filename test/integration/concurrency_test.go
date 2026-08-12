package integration_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConcurrency_DoubleSpendIsPrevented funds a wallet with exactly enough
// for 3 of 10 concurrently-fired transfers and asserts the row lock in
// docs/design.md §7 makes the outcome exact: precisely 3 succeed, precisely
// 7 fail with INSUFFICIENT_FUNDS, and the wallet never goes negative even
// transiently.
func TestConcurrency_DoubleSpendIsPrevented(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_1", "INR")
	createWallet(t, srv, "wallet_2", "INR")
	fundWallet(t, srv, "wallet_1", 100) // enough for exactly 3 transfers of 30

	const attempts = 10
	const amount = 30

	var wg sync.WaitGroup
	statuses := make([]int, attempts)
	failureCodes := make([]string, attempts)

	start := make(chan struct{})
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp := postJSON(t, srv.URL+"/transfers", map[string]any{
				"idempotencyKey": fmt.Sprintf("double-spend-%d", i),
				"fromWalletId":   "wallet_1",
				"toWalletId":     "wallet_2",
				"amount":         amount,
			})
			body := decodeJSON(t, resp)
			statuses[i] = resp.StatusCode
			if code, ok := body["failureCode"].(string); ok {
				failureCodes[i] = code
			}
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded, failed := 0, 0
	for i := range attempts {
		switch statuses[i] {
		case http.StatusCreated:
			succeeded++
		case http.StatusUnprocessableEntity:
			require.Equal(t, "INSUFFICIENT_FUNDS", failureCodes[i])
			failed++
		default:
			t.Fatalf("attempt %d: unexpected status %d (must never be 500 or a deadlock)", i, statuses[i])
		}
	}

	require.Equal(t, 3, succeeded, "exactly floor(100/30) = 3 transfers must succeed")
	require.Equal(t, 7, failed, "the remaining 7 must fail with insufficient funds, not double-spend")

	require.Equal(t, int64(10), getWalletBalance(t, db, "wallet_1"), "100 - 3*30 = 10, never negative")
	require.Equal(t, int64(90), getWalletBalance(t, db, "wallet_2"))

	assertInvariants(t, db)
}

// TestConcurrency_BidirectionalTransfersDoNotDeadlock runs A->B and B->A
// transfers concurrently. If the ascending-id lock ordering in
// docs/design.md §7 were absent, this reliably deadlocks; with it, every
// request must settle (win or lose on funds) and never surface a 500.
func TestConcurrency_BidirectionalTransfersDoNotDeadlock(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_a", "INR")
	createWallet(t, srv, "wallet_b", "INR")
	fundWallet(t, srv, "wallet_a", 100_000)
	fundWallet(t, srv, "wallet_b", 100_000)

	const perDirection = 30
	total := perDirection * 2

	var wg sync.WaitGroup
	statuses := make([]int, total)

	start := make(chan struct{})
	for i := range perDirection {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			resp := postJSON(t, srv.URL+"/transfers", map[string]any{
				"idempotencyKey": fmt.Sprintf("a-to-b-%d", i),
				"fromWalletId":   "wallet_a",
				"toWalletId":     "wallet_b",
				"amount":         10,
			})
			statuses[i] = resp.StatusCode
			resp.Body.Close()
		}(i)
		go func(i int) {
			defer wg.Done()
			<-start
			resp := postJSON(t, srv.URL+"/transfers", map[string]any{
				"idempotencyKey": fmt.Sprintf("b-to-a-%d", i),
				"fromWalletId":   "wallet_b",
				"toWalletId":     "wallet_a",
				"amount":         10,
			})
			statuses[perDirection+i] = resp.StatusCode
			resp.Body.Close()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, status := range statuses {
		require.NotEqual(t, http.StatusInternalServerError, status, "request %d must not fail with a 500 (deadlock)", i)
		require.Contains(t, []int{http.StatusCreated, http.StatusUnprocessableEntity}, status, "request %d got unexpected status %d", i, status)
	}

	// Equal amounts each way at ample balance: every transfer should
	// succeed, and the net movement should be zero.
	require.Equal(t, int64(100_000), getWalletBalance(t, db, "wallet_a"))
	require.Equal(t, int64(100_000), getWalletBalance(t, db, "wallet_b"))

	assertInvariants(t, db)
}
