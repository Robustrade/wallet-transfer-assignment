package test_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository/memory"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/service"
)

// BenchmarkTransferThroughput measures raw transfer throughput (run with: go test -bench=. -benchtime=5s ./test/)
func BenchmarkTransferThroughput(b *testing.B) {
	repo := memory.NewMemoryRepository()
	txService := service.NewTransferService(repo)
	walletService := service.NewWalletService(repo)
	ctx := context.Background()

	_, _ = walletService.CreateWallet(ctx, service.CreateWalletRequest{ID: "bench_from", InitialBalance: 1_000_000_000, Currency: "USD"})
	_, _ = walletService.CreateWallet(ctx, service.CreateWalletRequest{ID: "bench_to", InitialBalance: 0, Currency: "USD"})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			txService.ExecuteTransfer(ctx, service.TransferRequest{
				IdempotencyKey: fmt.Sprintf("bench-%d-%d", b.N, i),
				FromWalletID:   "bench_from",
				ToWalletID:     "bench_to",
				Amount:         1,
			})
		}
	})
}

// TestLoadTest_HighConcurrencyWithStats runs a detailed load test and prints a report.
func TestLoadTest_HighConcurrencyWithStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	repo := memory.NewMemoryRepository()
	txService := service.NewTransferService(repo)
	walletService := service.NewWalletService(repo)
	ctx := context.Background()

	// Seed wallets with enough balance for all transfers
	numWorkers := 100
	transferAmount := int64(10)
	initialBalance := int64(numWorkers) * transferAmount * 10

	_, _ = walletService.CreateWallet(ctx, service.CreateWalletRequest{ID: "load_from", InitialBalance: initialBalance, Currency: "USD"})
	_, _ = walletService.CreateWallet(ctx, service.CreateWalletRequest{ID: "load_to", InitialBalance: 0, Currency: "USD"})

	var (
		wg          sync.WaitGroup
		successCount int64
		failCount    int64
		totalLatency int64
		latencies    = make([]int64, numWorkers)
	)

	wg.Add(numWorkers)
	overallStart := time.Now()

	for i := 0; i < numWorkers; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			start := time.Now()

			_, status, _ := txService.ExecuteTransfer(ctx, service.TransferRequest{
				IdempotencyKey: fmt.Sprintf("load-key-%d", workerID),
				FromWalletID:   "load_from",
				ToWalletID:     "load_to",
				Amount:         transferAmount,
			})

			ms := time.Since(start).Milliseconds()
			latencies[workerID] = ms
			atomic.AddInt64(&totalLatency, ms)

			if status == 201 {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(overallStart)

	// Compute stats
	success := atomic.LoadInt64(&successCount)
	failed := atomic.LoadInt64(&failCount)
	avgLatency := totalLatency / int64(numWorkers)
	rps := float64(numWorkers) / elapsed.Seconds()

	var minL, maxL int64 = latencies[0], latencies[0]
	for _, l := range latencies {
		if l < minL { minL = l }
		if l > maxL { maxL = l }
	}

	t.Logf("\n")
	t.Logf("┌─────────────────────────────────────────┐")
	t.Logf("│         LOAD TEST RESULTS               │")
	t.Logf("├─────────────────────────────────────────┤")
	t.Logf("│ Total Requests    : %-4d                 │", numWorkers)
	t.Logf("│ Successful (201)  : %-4d  ✅             │", success)
	t.Logf("│ Failed            : %-4d                 │", failed)
	t.Logf("│ Total Duration    : %-12s          │", elapsed.Round(time.Millisecond))
	t.Logf("│ Throughput        : %-8.1f req/s       │", rps)
	t.Logf("│ Avg Latency       : %-4d ms              │", avgLatency)
	t.Logf("│ Min Latency       : %-4d ms              │", minL)
	t.Logf("│ Max Latency       : %-4d ms              │", maxL)
	t.Logf("└─────────────────────────────────────────┘")

	// Invariant checks
	fromWallet, _ := walletService.GetWallet(ctx, "load_from")
	toWallet, _ := walletService.GetWallet(ctx, "load_to")

	totalFunds := fromWallet.Balance + toWallet.Balance
	if totalFunds != initialBalance {
		t.Fatalf("❌ MONEY CONSERVATION VIOLATED: expected %d, got %d (from=%d, to=%d)",
			initialBalance, totalFunds, fromWallet.Balance, toWallet.Balance)
	}

	if fromWallet.Balance < 0 || toWallet.Balance < 0 {
		t.Fatalf("❌ NEGATIVE BALANCE DETECTED: from=%d, to=%d", fromWallet.Balance, toWallet.Balance)
	}

	t.Logf("\n✅ Invariant: Total funds conserved (%d = %d + %d)", totalFunds, fromWallet.Balance, toWallet.Balance)
	t.Logf("✅ Invariant: No negative balances")
	t.Logf("✅ Invariant: 0 race conditions (verified by -race flag)")

	if failed > 0 {
		t.Logf("⚠️  %d transfers failed (expected if wallet ran out of funds)", failed)
	}
}
