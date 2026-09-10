package test

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"wallet-transfer-assignment/internal/domain"
)

// TestConcurrency_DoubleSpendPrevention verifies that when 50 concurrent requests
// attempt to debit the same wallet with insufficient total balance, no race condition
// or double-spending occurs.
func TestConcurrency_DoubleSpendPrevention(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Sender has ₹100.00 (10,000 paise), Receiver has ₹0.00
	fromID := fmt.Sprintf("sender_stress_%d", time.Now().UnixNano())
	toID := fmt.Sprintf("receiver_stress_%d", time.Now().UnixNano())
	createTestWallet(t, ts, fromID, 10000)
	createTestWallet(t, ts, toID, 0)

	// 2. Act: 50 concurrent goroutines each attempting to transfer ₹50.00 (5,000 paise)
	const concurrentRequests = 50
	var (
		successCount atomic.Int32
		failCount    atomic.Int32
		wg           sync.WaitGroup
		startLatch   = make(chan struct{})
	)

	wg.Add(concurrentRequests)
	for i := 0; i < concurrentRequests; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startLatch // Synchronized release of all goroutines

			status, _, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("stress_key_%d_%d", time.Now().UnixNano(), idx),
				FromWalletID:   fromID,
				ToWalletID:     toID,
				Amount:         5000,
			})

			switch status {
			case http.StatusCreated:
				successCount.Add(1)
			case http.StatusUnprocessableEntity:
				failCount.Add(1)
			}
		}(i)
	}

	// Release all goroutines simultaneously
	close(startLatch)
	wg.Wait()

	// 3. Assert: Exactly 2 transfers must succeed (₹50 + ₹50 = ₹100 total), and 48 must fail
	assert.Equal(t, int32(2), successCount.Load(), "exactly 2 transfers of ₹50 should succeed from a ₹100 wallet")
	assert.Equal(t, int32(48), failCount.Load(), "48 transfers should fail with INSUFFICIENT_FUNDS")

	// 4. Assert: Final balances are strictly consistent
	sender := getTestWallet(t, ts, fromID)
	assert.Equal(t, int64(0), sender.Balance, "sender balance must be strictly ₹0 (never negative)")

	receiver := getTestWallet(t, ts, toID)
	assert.Equal(t, int64(10000), receiver.Balance, "receiver balance must be strictly ₹100")
}

// TestConcurrency_BidirectionalDeadlockSafety verifies that concurrent transfers
// in opposite directions between the same two wallets (A -> B and B -> A) do not deadlock.
func TestConcurrency_BidirectionalDeadlockSafety(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Two wallets with ₹500.00 (50,000 paise) each
	walletA := fmt.Sprintf("wallet_a_%d", time.Now().UnixNano())
	walletB := fmt.Sprintf("wallet_b_%d", time.Now().UnixNano())
	createTestWallet(t, ts, walletA, 50000)
	createTestWallet(t, ts, walletB, 50000)

	const transferCount = 20
	var (
		wg         sync.WaitGroup
		startLatch = make(chan struct{})
		errorsAtoB atomic.Int32
		errorsBtoA atomic.Int32
	)

	// 20 transfers: A -> B (₹10.00 each)
	wg.Add(transferCount)
	for i := 0; i < transferCount; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startLatch

			status, _, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("deadlock_ab_%d_%d", time.Now().UnixNano(), idx),
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         1000,
			})
			if status != http.StatusCreated {
				errorsAtoB.Add(1)
			}
		}(i)
	}

	// 20 transfers: B -> A (₹10.00 each)
	wg.Add(transferCount)
	for i := 0; i < transferCount; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startLatch

			status, _, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("deadlock_ba_%d_%d", time.Now().UnixNano(), idx),
				FromWalletID:   walletB,
				ToWalletID:     walletA,
				Amount:         1000,
			})
			if status != http.StatusCreated {
				errorsBtoA.Add(1)
			}
		}(i)
	}

	// Release all 40 goroutines simultaneously
	close(startLatch)
	wg.Wait()

	// 2. Assert: All 40 transfers succeeded without any deadlock errors
	assert.Equal(t, int32(0), errorsAtoB.Load(), "all A->B transfers must succeed without deadlock")
	assert.Equal(t, int32(0), errorsBtoA.Load(), "all B->A transfers must succeed without deadlock")

	// 3. Assert: Final balances remain balanced (₹500.00 each)
	finalA := getTestWallet(t, ts, walletA)
	finalB := getTestWallet(t, ts, walletB)
	assert.Equal(t, int64(50000), finalA.Balance)
	assert.Equal(t, int64(50000), finalB.Balance)
}

// TestConcurrency_DuplicateIdempotencyKeyRace verifies that when 10 goroutines
// concurrently submit the exact same idempotencyKey, exactly one transfer executes
// and all 10 receive the identical response.
func TestConcurrency_DuplicateIdempotencyKeyRace(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	fromID := fmt.Sprintf("sender_race_%d", time.Now().UnixNano())
	toID := fmt.Sprintf("receiver_race_%d", time.Now().UnixNano())
	createTestWallet(t, ts, fromID, 10000)
	createTestWallet(t, ts, toID, 1000)

	idempKey := fmt.Sprintf("race_key_%d", time.Now().UnixNano())
	req := domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         3000,
	}

	const racers = 10
	var (
		wg         sync.WaitGroup
		startLatch = make(chan struct{})
		results    = make([]struct {
			status int
			tr     *domain.Transfer
		}, racers)
	)

	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startLatch

			status, tr, _ := sendTransfer(t, ts, req)
			results[idx] = struct {
				status int
				tr     *domain.Transfer
			}{status: status, tr: tr}
		}(i)
	}

	close(startLatch)
	wg.Wait()

	// Assert: All racers received either 201 Created or 200 OK (replay)
	var createdCount, replayCount int
	var transferID string
	for _, res := range results {
		require.Contains(t, []int{http.StatusCreated, http.StatusOK}, res.status)
		require.NotNil(t, res.tr)
		if res.status == http.StatusCreated {
			createdCount++
		} else {
			replayCount++
		}
		if transferID == "" {
			transferID = res.tr.ID
		} else {
			assert.Equal(t, transferID, res.tr.ID, "all responses must return the same transfer ID")
		}
	}

	// At least 1 created and the rest replayed
	assert.Equal(t, 1, createdCount, "exactly 1 request should create the transfer")
	assert.Equal(t, racers-1, replayCount, "remaining requests should be replayed")

	// Balances debited exactly once
	assert.Equal(t, int64(7000), getTestWallet(t, ts, fromID).Balance)
	assert.Equal(t, int64(4000), getTestWallet(t, ts, toID).Balance)

	// Exactly 2 ledger entries exist in database
	ledgerEntries := getTestLedgerEntries(t, pool, transferID)
	require.Len(t, ledgerEntries, 2)
}

// TestConcurrency_InsufficientFunds_DuplicateKeyRace verifies that when 10 goroutines
// concurrently submit the exact same request with insufficient funds and identical idempotencyKey,
// all 10 receive 422 Unprocessable Entity (no 500 internal errors) and exactly 1 failed transfer is recorded.
func TestConcurrency_InsufficientFunds_DuplicateKeyRace(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	fromID := fmt.Sprintf("sender_insuf_race_%d", time.Now().UnixNano())
	toID := fmt.Sprintf("receiver_insuf_race_%d", time.Now().UnixNano())
	createTestWallet(t, ts, fromID, 500)
	createTestWallet(t, ts, toID, 1000)

	idempKey := fmt.Sprintf("insuf_race_key_%d", time.Now().UnixNano())
	req := domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         1000,
	}

	const racers = 10
	var (
		wg         sync.WaitGroup
		startLatch = make(chan struct{})
		results    = make([]struct {
			status  int
			headers http.Header
			errBody map[string]any
		}, racers)
	)

	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startLatch

			status, headers, _, errBody := sendTransferFull(t, ts, req)
			results[idx] = struct {
				status  int
				headers http.Header
				errBody map[string]any
			}{status: status, headers: headers, errBody: errBody}
		}(i)
	}

	close(startLatch)
	wg.Wait()

	// Assert: All 10 racers received 422 Unprocessable Entity with INSUFFICIENT_FUNDS
	var replayCount int
	for _, res := range results {
		require.Equal(t, http.StatusUnprocessableEntity, res.status)
		require.NotNil(t, res.errBody)
		assert.Equal(t, domain.ErrReasonInsufficientFunds, res.errBody["error"])
		if res.headers.Get("X-Idempotent-Replay") == "true" {
			replayCount++
		}
	}

	// At least 1 initial failure and other racers replayed safely without 500 errors
	assert.GreaterOrEqual(t, replayCount, 1, "at least one racer should be an idempotent replay")

	// Balances remain untouched
	assert.Equal(t, int64(500), getTestWallet(t, ts, fromID).Balance)
	assert.Equal(t, int64(1000), getTestWallet(t, ts, toID).Balance)

	// Exactly 1 failed transfer record in DB
	failedTransfer := getTestTransferByIdempotencyKey(t, pool, idempKey)
	assert.Equal(t, domain.StatusFailed, failedTransfer.Status)

	// Zero ledger entries in DB
	ledgerEntries := getTestLedgerEntries(t, pool, failedTransfer.ID)
	assert.Empty(t, ledgerEntries)
}
