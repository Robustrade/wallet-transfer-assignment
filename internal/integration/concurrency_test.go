package integration_test

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/service"
)

// runConcurrently starts n goroutines at the same instant and waits for all of
// them. Workers receive a context that expires after timeout; they must pass it
// to their database and HTTP calls. If the deadline passes (a probable
// deadlock), the in-flight calls are cancelled, the workers are joined, and only
// then does the test fail, so no goroutine is left holding a connection when
// test cleanup closes the pool.
func runConcurrently(t *testing.T, n int, timeout time.Duration, fn func(ctx context.Context, i int)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			fn(ctx, i)
		}()
	}
	ready.Wait()
	close(start)

	finished := make(chan struct{})
	go func() { done.Wait(); close(finished) }()

	select {
	case <-finished:
	case <-ctx.Done():
		// Cancelled calls return promptly, so this join is short.
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
		}
		t.Fatalf("goroutines did not finish within %s: probable deadlock", timeout)
	}
	if ctx.Err() != nil {
		t.Fatalf("goroutines hit the %s deadline: probable deadlock", timeout)
	}
}

func TestConcurrent_DebitsFromOneWalletNeverOverspend(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "src", 1000)
	e.wallet(t, "dst", 0)

	const attempts = 50 // each moves 100, so only 10 can be afforded
	results := make([]*service.TransferResult, attempts)
	errs := make([]error, attempts)
	runConcurrently(t, attempts, time.Minute, func(ctx context.Context, i int) {
		results[i], errs[i] = e.transfers.CreateTransfer(ctx,
			transferInput(fmt.Sprintf("key-%d", i), "src", "dst", 100))
	})

	processed, failed := 0, 0
	for i := range results {
		require.NoError(t, errs[i])
		switch results[i].Transfer.Status {
		case domain.TransferProcessed:
			processed++
		case domain.TransferFailed:
			failed++
			assert.Equal(t, domain.FailureInsufficientFunds, results[i].Transfer.FailureReason)
		}
	}
	assert.Equal(t, 10, processed, "exactly the affordable transfers succeed")
	assert.Equal(t, attempts-10, failed)
	assert.EqualValues(t, 0, e.balance(t, "src"))
	assert.EqualValues(t, 1000, e.balance(t, "dst"))
	assert.Equal(t, 20, e.count(t, "ledger_entries"))
	e.assertLedgerReconciles(t)
}

func TestConcurrent_OpposingTransfersDoNotDeadlock(t *testing.T) {
	e := newEnv(t)
	e.wallet(t, "a", 500)
	e.wallet(t, "b", 500)

	const attempts = 100
	errs := make([]error, attempts)
	runConcurrently(t, attempts, time.Minute, func(ctx context.Context, i int) {
		from, to := "a", "b"
		if i%2 == 1 {
			from, to = to, from
		}
		_, errs[i] = e.transfers.CreateTransfer(ctx,
			transferInput(fmt.Sprintf("key-%d", i), from, to, 1))
	})

	for _, err := range errs {
		require.NoError(t, err)
	}
	// 50 transfers each way of the same size cancel out.
	assert.EqualValues(t, 500, e.balance(t, "a"))
	assert.EqualValues(t, 500, e.balance(t, "b"))
	e.assertLedgerReconciles(t)
}

func TestConcurrent_RandomTransfersConserveMoney(t *testing.T) {
	e := newEnv(t)
	ids := []string{"w0", "w1", "w2", "w3", "w4", "w5"}
	for _, id := range ids {
		e.wallet(t, id, 1000)
	}

	const attempts = 200
	errs := make([]error, attempts)
	runConcurrently(t, attempts, 2*time.Minute, func(ctx context.Context, i int) {
		rng := rand.New(rand.NewSource(int64(i)))
		from := rng.Intn(len(ids))
		to := (from + 1 + rng.Intn(len(ids)-1)) % len(ids)
		_, errs[i] = e.transfers.CreateTransfer(ctx,
			transferInput(fmt.Sprintf("key-%d", i), ids[from], ids[to], int64(1+rng.Intn(400))))
	})

	for _, err := range errs {
		require.NoError(t, err)
	}
	var total int64
	for _, id := range ids {
		b := e.balance(t, id)
		assert.GreaterOrEqual(t, b, int64(0))
		total += b
	}
	assert.EqualValues(t, 6000, total, "money is neither created nor destroyed")
	e.assertLedgerReconciles(t)
}
