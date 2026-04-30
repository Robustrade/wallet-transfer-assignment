//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// debitCounters bundles the three per-outcome counters used by C1.
type debitCounters struct {
	success *atomic.Int32
	fail    *atomic.Int32
	other   *atomic.Int32
}

// runDebitGoroutine performs a single transfer in C1 and tallies the outcome.
// Extracted from the C1 goroutine body to keep cognitive complexity low.
func runDebitGoroutine(t *testing.T, env *testEnv, i int, amount int64, c debitCounters, errs chan<- string) {
	t.Helper()
	status, body, err := env.callTransfer(transferReq{
		IdempotencyKey: fmt.Sprintf("c1-%d", i),
		FromWalletID:   "source",
		ToWalletID:     "sink",
		Amount:         amount,
	})
	if err != nil {
		errs <- fmt.Sprintf("goroutine %d: %v", i, err)
		return
	}
	classifyDebitOutcome(t, i, status, body, c, errs)
}

// classifyDebitOutcome buckets the HTTP outcome of one C1 transfer.
func classifyDebitOutcome(t *testing.T, i int, status int, body []byte, c debitCounters, errs chan<- string) {
	t.Helper()
	switch status {
	case http.StatusCreated:
		c.success.Add(1)
	case http.StatusUnprocessableEntity:
		var resp transferResp
		if uerr := json.Unmarshal(body, &resp); uerr != nil {
			errs <- fmt.Sprintf("goroutine %d: unmarshal: %v", i, uerr)
			return
		}
		if resp.Status == "FAILED" {
			c.fail.Add(1)
		} else {
			c.other.Add(1)
		}
	default:
		c.other.Add(1)
		t.Logf("unexpected status %d body=%s", status, body)
	}
}

// C1 — N concurrent debits on the same wallet.
//
// Initial balance is exactly 30 * amount. We launch N=50 goroutines that each
// transfer `amount` out of the source wallet. We expect exactly 30 to succeed
// and 20 to fail with INSUFFICIENT_FUNDS. Final balance is 0; the ledger
// contains exactly 60 rows (30 DEBIT + 30 CREDIT).
func TestC1_ConcurrentDebitsSameWallet(t *testing.T) {
	env := setupTestEnv(t)
	const (
		amount       = int64(100)
		successful   = 30
		concurrent   = 50
		initialFunds = amount * successful
	)

	env.seedWallet("source", "INR", initialFunds)
	env.seedWallet("sink", "INR", 0)

	var (
		wg         sync.WaitGroup
		successCnt atomic.Int32
		failCnt    atomic.Int32
		other      atomic.Int32
	)
	errs := make(chan string, concurrent)
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runDebitGoroutine(t, env, i, amount, debitCounters{
				success: &successCnt,
				fail:    &failCnt,
				other:   &other,
			}, errs)
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("transfer error: %s", e)
	}
	if t.Failed() {
		return
	}

	assert.Equal(t, int32(successful), successCnt.Load(), "exactly %d transfers should succeed", successful)
	assert.Equal(t, int32(concurrent-successful), failCnt.Load(), "the rest should be FAILED")
	assert.Equal(t, int32(0), other.Load(), "no other outcomes expected")

	assert.Equal(t, int64(0), env.walletBalance("source"), "source wallet must be drained")
	assert.Equal(t, initialFunds, env.walletBalance("sink"), "sink wallet must hold the moved funds")
	assert.Equal(t, 2*successful, env.ledgerCount())
	assert.Equal(t, amount*successful, env.ledgerSumByType("DEBIT"))
	assert.Equal(t, amount*successful, env.ledgerSumByType("CREDIT"))
}

// C2 — N goroutines submit the same idempotencyKey concurrently.
//
// Exactly one transfer row exists; exactly two ledger rows exist; balances
// reflect a single transfer; all responses carry the same id and status.
func TestC2_ConcurrentSameIdempotencyKey(t *testing.T) {
	env := setupTestEnv(t)
	env.seedWallet("source", "INR", 10_000)
	env.seedWallet("sink", "INR", 0)

	const concurrent = 100

	var (
		wg       sync.WaitGroup
		bodies   = make([][]byte, concurrent)
		statuses = make([]int, concurrent)
	)
	errs := make(chan string, concurrent)
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, body, err := env.callTransfer(transferReq{
				IdempotencyKey: "shared-key",
				FromWalletID:   "source",
				ToWalletID:     "sink",
				Amount:         100,
			})
			if err != nil {
				errs <- fmt.Sprintf("goroutine %d: %v", i, err)
				return
			}
			statuses[i] = status
			bodies[i] = body
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("transfer error: %s", e)
	}
	if t.Failed() {
		return
	}

	// Exactly one 201 (the winner) and the rest 200 (replays).
	var created, replayed int
	for i, s := range statuses {
		var r transferResp
		if err := json.Unmarshal(bodies[i], &r); err != nil {
			t.Fatalf("unmarshal[%d]: %v", i, err)
		}
		switch s {
		case http.StatusCreated:
			created++
			assert.False(t, r.Replayed)
		case http.StatusOK:
			replayed++
			assert.True(t, r.Replayed)
		default:
			t.Errorf("unexpected status %d body=%s", s, bodies[i])
		}
	}
	assert.Equal(t, 1, created, "exactly one fresh transfer")
	assert.Equal(t, concurrent-1, replayed, "all others are replays")

	assert.Equal(t, 1, env.transferCount())
	assert.Equal(t, 2, env.ledgerCount())
	assert.Equal(t, int64(9_900), env.walletBalance("source"))
	assert.Equal(t, int64(100), env.walletBalance("sink"))
}

// C3 — Cross-wallet deadlock probe.
//
// Half the goroutines transfer A→B, the other half B→A. With ID-ordered
// SELECT FOR UPDATE we should never deadlock. The total flow is symmetric
// (initial balance is large enough for everything to succeed), and we confirm
// no DB-level deadlock errors by asserting every request succeeded.
func TestC3_CrossWalletNoDeadlock(t *testing.T) {
	env := setupTestEnv(t)

	const (
		eachWay  = 50
		amount   = int64(10)
		startBal = int64(2_000)
	)
	env.seedWallet("wallet_a", "INR", startBal)
	env.seedWallet("wallet_b", "INR", startBal)

	var wg sync.WaitGroup
	errors := make(chan string, 2*eachWay)

	send := func(from, to, key string) {
		defer wg.Done()
		status, body, err := env.callTransfer(transferReq{
			IdempotencyKey: key,
			FromWalletID:   from,
			ToWalletID:     to,
			Amount:         amount,
		})
		if err != nil {
			errors <- fmt.Sprintf("err: %v", err)
			return
		}
		if status != http.StatusCreated {
			errors <- fmt.Sprintf("status=%d body=%s", status, body)
		}
	}

	for i := 0; i < eachWay; i++ {
		wg.Add(2)
		go send("wallet_a", "wallet_b", fmt.Sprintf("ab-%d", i))
		go send("wallet_b", "wallet_a", fmt.Sprintf("ba-%d", i))
	}
	wg.Wait()
	close(errors)

	for e := range errors {
		t.Errorf("transfer error: %s", e)
	}
	if t.Failed() {
		return
	}

	// Net balances unchanged because the flows cancel out.
	assert.Equal(t, startBal, env.walletBalance("wallet_a"))
	assert.Equal(t, startBal, env.walletBalance("wallet_b"))
	assert.Equal(t, 2*eachWay, env.transferCount())
	assert.Equal(t, 4*eachWay, env.ledgerCount())
	assert.Equal(t, env.ledgerSumByType("DEBIT"), env.ledgerSumByType("CREDIT"))
}

// C4 — Mixed: half are same-key replays, half are unique transfers.
//
// Asserts the system invariants from I3 still hold under mixed load.
func TestC4_MixedConcurrency(t *testing.T) {
	env := setupTestEnv(t)
	env.seedWallet("wallet_a", "INR", 10_000)
	env.seedWallet("wallet_b", "INR", 0)

	const replays = 40
	const uniques = 40

	var wg sync.WaitGroup

	errs := make(chan string, replays+uniques)

	// Replays: all share the key "shared".
	for i := 0; i < replays; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := env.callTransfer(transferReq{
				IdempotencyKey: "shared",
				FromWalletID:   "wallet_a",
				ToWalletID:     "wallet_b",
				Amount:         100,
			}); err != nil {
				errs <- fmt.Sprintf("replay %d: %v", i, err)
			}
		}(i)
	}

	// Uniques: each has its own key.
	for i := 0; i < uniques; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := env.callTransfer(transferReq{
				IdempotencyKey: fmt.Sprintf("u-%d", i),
				FromWalletID:   "wallet_a",
				ToWalletID:     "wallet_b",
				Amount:         100,
			}); err != nil {
				errs <- fmt.Sprintf("unique %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("transfer error: %s", e)
	}
	if t.Failed() {
		return
	}

	// Exactly 1 + uniques transfer rows; 2 * (1 + uniques) ledger rows.
	wantTransfers := 1 + uniques
	assert.Equal(t, wantTransfers, env.transferCount())
	assert.Equal(t, 2*wantTransfers, env.ledgerCount())

	// Ledger zero-sum and stored-balance == initial + ledger net.
	assert.Equal(t, env.ledgerSumByType("DEBIT"), env.ledgerSumByType("CREDIT"))
	expectedA := int64(10_000) + env.ledgerNetForWallet("wallet_a")
	expectedB := int64(0) + env.ledgerNetForWallet("wallet_b")
	assert.Equal(t, expectedA, env.walletBalance("wallet_a"))
	assert.Equal(t, expectedB, env.walletBalance("wallet_b"))
}
