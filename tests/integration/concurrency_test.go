//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"sync"
	"testing"
)

// TestConcurrentTransfers_NoDoubleSpend verifies that concurrent transfers
// from the same wallet cannot spend more than the available balance.
// 10 goroutines each attempt to transfer 200 from a wallet with 1000.
// Exactly 5 should succeed (1000/200), the rest should fail with insufficient funds.
func TestConcurrentTransfers_NoDoubleSpend(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_rich", 1000)
	createTestWallet(t, "wallet_dest", 0)

	const numGoroutines = 10
	const amount = 200

	var wg sync.WaitGroup
	successes := make(chan int, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			body := fmt.Sprintf(
				`{"idempotencyKey":"concurrent-%d","fromWalletId":"wallet_rich","toWalletId":"wallet_dest","amount":%d}`,
				idx, amount,
			)
			resp, err := http.Post(
				testServer.URL+"/transfers",
				"application/json",
				bytes.NewBufferString(body),
			)
			if err != nil {
				successes <- 0
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusCreated {
				successes <- 1
			} else {
				successes <- 0
			}
		}(i)
	}

	wg.Wait()
	close(successes)

	successCount := 0
	for s := range successes {
		successCount += s
	}

	// Exactly 5 transfers should succeed: 1000 / 200 = 5
	assertEqual(t, 5, successCount, "number of successful concurrent transfers")

	// Verify final balances
	walletRich := getWallet(t, "wallet_rich")
	walletDest := getWallet(t, "wallet_dest")
	assertEqual(t, int64(0), walletRich.Balance, "wallet_rich final balance")
	assertEqual(t, int64(1000), walletDest.Balance, "wallet_dest final balance")

	// Verify total money is conserved
	assertEqual(t, int64(1000), walletRich.Balance+walletDest.Balance, "total money conserved")
}

// TestConcurrentTransfers_LedgerBalances verifies that after concurrent transfers,
// the sum of all DEBIT entries equals the sum of all CREDIT entries (ledger balances).
func TestConcurrentTransfers_LedgerBalances(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_src", 5000)
	createTestWallet(t, "wallet_dst", 0)

	const numTransfers = 20
	const amount = 100

	var wg sync.WaitGroup
	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			body := fmt.Sprintf(
				`{"idempotencyKey":"ledger-concurrent-%d","fromWalletId":"wallet_src","toWalletId":"wallet_dst","amount":%d}`,
				idx, amount,
			)
			resp, err := http.Post(
				testServer.URL+"/transfers",
				"application/json",
				bytes.NewBufferString(body),
			)
			if err != nil {
				return
			}
			resp.Body.Close()
		}(i)
	}

	wg.Wait()

	// Verify ledger balance: total debits == total credits
	rows, err := testDB.Pool.Query(context.Background(),
		"SELECT entry_type, SUM(amount) FROM ledger_entries GROUP BY entry_type",
	)
	if err != nil {
		t.Fatalf("failed to query ledger sums: %v", err)
	}
	defer rows.Close()

	var totalDebit, totalCredit int64
	for rows.Next() {
		var entryType string
		var sum int64
		if err := rows.Scan(&entryType, &sum); err != nil {
			t.Fatalf("failed to scan: %v", err)
		}
		switch entryType {
		case "DEBIT":
			totalDebit = sum
		case "CREDIT":
			totalCredit = sum
		}
	}

	assertEqual(t, totalDebit, totalCredit, "ledger balance (total debits == total credits)")

	if totalDebit == 0 {
		t.Error("expected some transfers to succeed, but total debit is 0")
	}
}

// TestConcurrentTransfers_BidirectionalNoDeadlock verifies that concurrent
// transfers in opposite directions (A→B and B→A) complete without deadlocks.
// The deterministic wallet ordering (lock by ID) should prevent this.
func TestConcurrentTransfers_BidirectionalNoDeadlock(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_x", 10000)
	createTestWallet(t, "wallet_y", 10000)

	const numPairs = 10
	const amount = 100

	var wg sync.WaitGroup
	for i := 0; i < numPairs; i++ {
		// Transfer X → Y
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := fmt.Sprintf(
				`{"idempotencyKey":"bidir-xy-%d","fromWalletId":"wallet_x","toWalletId":"wallet_y","amount":%d}`,
				idx, amount,
			)
			resp, err := http.Post(testServer.URL+"/transfers", "application/json", bytes.NewBufferString(body))
			if err != nil {
				return
			}
			resp.Body.Close()
		}(i)

		// Transfer Y → X
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := fmt.Sprintf(
				`{"idempotencyKey":"bidir-yx-%d","fromWalletId":"wallet_y","toWalletId":"wallet_x","amount":%d}`,
				idx, amount,
			)
			resp, err := http.Post(testServer.URL+"/transfers", "application/json", bytes.NewBufferString(body))
			if err != nil {
				return
			}
			resp.Body.Close()
		}(i)
	}

	wg.Wait()

	// All transfers should succeed since both wallets have ample funds
	walletX := getWallet(t, "wallet_x")
	walletY := getWallet(t, "wallet_y")

	// Total money conserved: 10000 + 10000 = 20000
	assertEqual(t, int64(20000), walletX.Balance+walletY.Balance, "total money conserved after bidirectional transfers")

	// Each wallet should have exactly: initial - (numPairs * amount) + (numPairs * amount) = initial
	assertEqual(t, int64(10000), walletX.Balance, "wallet_x balance unchanged after equal bidirectional transfers")
	assertEqual(t, int64(10000), walletY.Balance, "wallet_y balance unchanged after equal bidirectional transfers")
}

// TestConcurrentIdempotency_SameKeyFromMultipleGoroutines verifies that
// submitting the same idempotency key concurrently results in exactly one transfer.
func TestConcurrentIdempotency_SameKeyFromMultipleGoroutines(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 5000)
	createTestWallet(t, "wallet_b", 0)

	const numGoroutines = 10

	var wg sync.WaitGroup
	transferIDs := make(chan string, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			body := `{"idempotencyKey":"same-key","fromWalletId":"wallet_a","toWalletId":"wallet_b","amount":500}`
			resp, err := http.Post(testServer.URL+"/transfers", "application/json", bytes.NewBufferString(body))
			if err != nil {
				return
			}
			defer resp.Body.Close()

			// Both 201 (new) and 200 (replay) are successful responses
			if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
				var result apiResponse
				json.NewDecoder(resp.Body).Decode(&result)
				var transfer transferData
				json.Unmarshal(result.Data, &transfer)
				transferIDs <- transfer.ID
			}
		}()
	}

	wg.Wait()
	close(transferIDs)

	// Collect all transfer IDs
	ids := make(map[string]bool)
	for id := range transferIDs {
		ids[id] = true
	}

	// All successful responses should have the SAME transfer ID
	assertEqual(t, 1, len(ids), "unique transfer IDs (should all be the same)")

	// Balance should only be debited once: 5000 - 500 = 4500
	walletA := getWallet(t, "wallet_a")
	assertEqual(t, int64(4500), walletA.Balance, "wallet_a balance (debited once)")

	// Only 1 transfer record should exist
	var transferCount int
	testDB.Pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM transfers WHERE idempotency_key = 'same-key'",
	).Scan(&transferCount)
	assertEqual(t, 1, transferCount, "transfer record count")
}

// TestConcurrentIdempotency_FastFail409 verifies that a concurrent request
// using the same idempotency key fails fast with 409 Conflict instead of blocking.
// We simulate this by holding a transaction-level advisory lock on the key hash
// in a separate test database connection, then making the HTTP request.
func TestConcurrentIdempotency_FastFail409(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 500)

	key := "fast-fail-key-123"

	// Compute FNV-1a 64-bit hash of the key (matching repository/idempotency_repo.go implementation)
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	lockID := int64(h.Sum64())

	// Start a transaction in the test's own DB session to acquire the lock
	ctx := context.Background()
	tx, err := testDB.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin test transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	// Acquire the transaction-level advisory lock on behalf of the test
	var acquired bool
	err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", lockID).Scan(&acquired)
	if err != nil {
		t.Fatalf("failed to call pg_try_advisory_xact_lock in test: %v", err)
	}
	if !acquired {
		t.Fatal("expected test to acquire the advisory lock successfully")
	}

	// While we hold the lock, send an HTTP POST request to the server with the same key.
	// The server should fail to acquire the lock and return 409 Conflict immediately.
	body := fmt.Sprintf(
		`{"idempotencyKey":"%s","fromWalletId":"wallet_a","toWalletId":"wallet_b","amount":100}`,
		key,
	)
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusConflict, resp.StatusCode, "HTTP status for concurrent lock failure")

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	assertEqual(t, false, result.Success, "success status")
	if result.Error == nil {
		t.Fatal("expected error object, got nil")
	}
	assertEqual(t, "IDEMPOTENCY_KEY_IN_PROGRESS", result.Error.Code, "error code")

	// Release the lock by rolling back/committing the test transaction
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback test transaction: %v", err)
	}

	// Now that the lock is released, try the transfer again. It should succeed (201 Created).
	resp2 := doHTTPPost(t, "/transfers", body)
	defer resp2.Body.Close()

	assertStatusCode(t, http.StatusCreated, resp2.StatusCode, "HTTP status after lock release")
}
