//go:build stress

// Build tag `stress` because these run for tens of seconds and
// shouldn't fire in the normal CI loop. Invoke via:
//
//	make stress
//	# or
//	go test -race -tags='integration stress' -count=1 -timeout=600s \
//	    -run='Stress|NoGoroutineLeak' ./internal/service/...
//
// They share testdb with the regular integration tests, so the
// container is reused; only the per-test container reset cost is paid.
package service_test

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

// TestStress_HotWallet hammers a single source wallet with 1,000
// concurrent debit attempts. The math is set up so only ~100 can
// succeed; the other 900 must fail cleanly with FAILED state and
// the source balance must never go negative.
//
// This is the headline "no double-spend under heavy contention"
// proof. It scales the original TestCreateTransfer_ConcurrentDebits
// (20 goroutines) up by 50x.
//
// Invariants asserted:
//   - source balance never goes below zero,
//   - source + destination sum equals the starting balance
//     (no money created or destroyed),
//   - exactly floor(starting_balance / per_transfer) PROCESSED outcomes,
//   - all other transfers terminate in FAILED (no errors, no panics),
//   - no transfer ends up still PENDING.
func TestStress_HotWallet(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()

	const (
		startingBalance int64 = 10_000
		perTransfer     int64 = 100
		numGoroutines         = 1_000
	)
	seedWallets(t, wsvc, map[string]int64{"hot": startingBalance, "sink": 0})

	var (
		processed, failed atomic.Int64
		wg                sync.WaitGroup
	)
	start := time.Now()
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
				IdempotencyKey: "hot-" + strconv.Itoa(i),
				FromWalletID:   "hot",
				ToWalletID:     "sink",
				Amount:         perTransfer,
			})
			if err != nil {
				t.Errorf("transfer %d: %v", i, err)
				return
			}
			switch tr.State {
			case domain.StateProcessed:
				processed.Add(1)
			case domain.StateFailed:
				failed.Add(1)
			default:
				t.Errorf("transfer %d unexpected state %s", i, tr.State)
			}
		}(i)
	}
	wg.Wait()
	dur := time.Since(start)

	src, _ := wsvc.Get(ctx, "hot")
	dst, _ := wsvc.Get(ctx, "sink")

	if src.Balance < 0 {
		t.Fatalf("source went negative: %d", src.Balance)
	}
	if src.Balance+dst.Balance != startingBalance {
		t.Fatalf("ledger not conserved: src=%d dst=%d (sum=%d, want %d)",
			src.Balance, dst.Balance, src.Balance+dst.Balance, startingBalance)
	}
	wantProcessed := startingBalance / perTransfer
	if processed.Load() != wantProcessed {
		t.Fatalf("processed: got %d want %d", processed.Load(), wantProcessed)
	}
	if processed.Load()+failed.Load() != int64(numGoroutines) {
		t.Fatalf("processed=%d failed=%d total=%d want %d",
			processed.Load(), failed.Load(),
			processed.Load()+failed.Load(), numGoroutines)
	}

	t.Logf("stress=hot_wallet goroutines=%d processed=%d failed=%d elapsed=%s rate=%.0f/s",
		numGoroutines, processed.Load(), failed.Load(), dur,
		float64(numGoroutines)/dur.Seconds())
}

// TestStress_ManyWallets exercises the realistic-mix case: 100
// wallets each seeded with 1,000, 10,000 random transfers between
// random pairs by 50 goroutines.
//
// The point is the conservation invariant: across this many
// transactions, the SUM of all wallet balances must equal the SUM
// of the initial balances. Any leak (over-spend, double-spend,
// lost credit) shows up as a sum mismatch and fails the test.
//
// This is the closest in-process equivalent to a sustained
// production write workload.
func TestStress_ManyWallets(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()

	const (
		numWallets        = 100
		perWalletBalance  = int64(1_000)
		numTransfers      = 10_000
		concurrentWorkers = 50
		transferAmountMin = int64(1)
		transferAmountMax = int64(50)
	)
	totalInitial := int64(numWallets) * perWalletBalance

	// Seed N wallets.
	seed := make(map[string]int64, numWallets)
	for i := 0; i < numWallets; i++ {
		seed[fmt.Sprintf("w%03d", i)] = perWalletBalance
	}
	seedWallets(t, wsvc, seed)

	// A small deterministic PRNG so the test is reproducible.
	// We hand-roll instead of importing math/rand to keep this
	// file dependency-free; the quality is fine for picking
	// (from, to, amount) triples.
	var seed64 uint64 = 0xC0FFEE_BABE
	next := func() uint64 {
		seed64 ^= seed64 << 13
		seed64 ^= seed64 >> 7
		seed64 ^= seed64 << 17
		return seed64
	}

	// Generate the work upfront so every goroutine just executes
	// already-built requests; this isolates the test from any
	// scheduling artefacts in request construction.
	type job struct {
		idx      int
		from, to string
		amount   int64
	}
	jobs := make([]job, numTransfers)
	for i := 0; i < numTransfers; i++ {
		fromIdx := int(next() % uint64(numWallets))
		toIdx := int(next() % uint64(numWallets))
		for fromIdx == toIdx {
			toIdx = int(next() % uint64(numWallets))
		}
		amt := transferAmountMin + int64(next()%uint64(transferAmountMax-transferAmountMin+1))
		jobs[i] = job{
			idx:    i,
			from:   fmt.Sprintf("w%03d", fromIdx),
			to:     fmt.Sprintf("w%03d", toIdx),
			amount: amt,
		}
	}

	ch := make(chan job, concurrentWorkers*2)
	var (
		processed, failed atomic.Int64
		wg                sync.WaitGroup
	)
	start := time.Now()
	for w := 0; w < concurrentWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
					IdempotencyKey: "stress-" + strconv.Itoa(j.idx),
					FromWalletID:   j.from,
					ToWalletID:     j.to,
					Amount:         j.amount,
				})
				if err != nil {
					t.Errorf("transfer %d: %v", j.idx, err)
					continue
				}
				switch tr.State {
				case domain.StateProcessed:
					processed.Add(1)
				case domain.StateFailed:
					failed.Add(1)
				default:
					t.Errorf("transfer %d unexpected state %s", j.idx, tr.State)
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	dur := time.Since(start)

	// Sum every wallet's balance — must equal the original total
	// regardless of how many transfers landed as PROCESSED vs
	// FAILED. This is the conservation invariant.
	var sum int64
	for id := range seed {
		w, err := wsvc.Get(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if w.Balance < 0 {
			t.Fatalf("wallet %s went negative: %d", id, w.Balance)
		}
		sum += w.Balance
	}
	if sum != totalInitial {
		t.Fatalf("conservation broken: sum=%d want %d (delta=%d)",
			sum, totalInitial, sum-totalInitial)
	}
	if processed.Load()+failed.Load() != int64(numTransfers) {
		t.Fatalf("processed=%d failed=%d total=%d want %d",
			processed.Load(), failed.Load(),
			processed.Load()+failed.Load(), numTransfers)
	}

	t.Logf("stress=many_wallets wallets=%d transfers=%d workers=%d "+
		"processed=%d failed=%d elapsed=%s rate=%.0f/s",
		numWallets, numTransfers, concurrentWorkers,
		processed.Load(), failed.Load(), dur,
		float64(numTransfers)/dur.Seconds())
}

// TestStress_NoGoroutineLeak verifies that running the hot-wallet
// stress test does NOT leak goroutines. A leak would show up as a
// gradual climb in NumGoroutine over a long-running service.
//
// We snapshot NumGoroutine before + after, allowing a small slack
// for runtime-managed background goroutines that may scale up.
func TestStress_NoGoroutineLeak(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()

	// Settle the runtime: give whatever was running time to exit.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	seedWallets(t, wsvc, map[string]int64{"src": 5_000, "dst": 0})

	const numGoroutines = 200
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = tsvc.Create(ctx, service.CreateTransferRequest{
				IdempotencyKey: "leak-" + strconv.Itoa(i),
				FromWalletID:   "src",
				ToWalletID:     "dst",
				Amount:         1,
			})
		}(i)
	}
	wg.Wait()

	// Give the runtime + pgx pool a moment to retire any work
	// goroutines spawned during the test.
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow a small upward drift; pgx maintains pool worker
	// goroutines that may scale up. 10 is generous.
	const slack = 10
	if after-before > slack {
		t.Fatalf("possible goroutine leak: before=%d after=%d (delta=%d > slack %d)",
			before, after, after-before, slack)
	}
	t.Logf("goroutine count: before=%d after=%d delta=%d",
		before, after, after-before)
}
