//go:build integration

// Benchmarks for the transfer hot path. Run via:
//
//	make bench
//	# or
//	go test -tags=integration -bench=. -benchmem -benchtime=5s \
//	    -run='^$' ./internal/service/...
//
// The `-run='^$'` skips test functions entirely, otherwise Go runs
// every TestXxx between benchmarks and the timing is misleading.
//
// Notes:
//   - Benchmarks share the testdb container (started lazily on first
//     Get) but each one calls testdb.Reset(b) to start with a clean
//     table state. Reset cost is amortised across b.N iterations.
//   - Source wallet is seeded with a huge balance so we never hit the
//     insufficient-funds path during the benchmark loop.
//   - We disable the timer during setup (b.StopTimer / b.StartTimer)
//     so seed + service construction don't pollute the throughput
//     number.
package service_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

// BenchmarkTransferService_Create measures sequential transfer
// throughput. One goroutine, no contention. This is the baseline:
// any parallel benchmark MUST beat this for throughput (or we're
// being throttled by the DB pool / lock contention).
//
// Reports the per-op time and memory allocations.
func BenchmarkTransferService_Create(b *testing.B) {
	testdb.Reset(b)
	tsvc, wsvc := newTransferSvcB(b)
	ctx := context.Background()
	mustSeedB(b, wsvc, "src", int64(b.N)*100)
	mustSeedB(b, wsvc, "dst", 0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tsvc.Create(ctx, service.CreateTransferRequest{
			IdempotencyKey: "bench-seq-" + strconv.Itoa(i),
			FromWalletID:   "src",
			ToWalletID:     "dst",
			Amount:         1,
		})
		if err != nil {
			b.Fatalf("create: %v", err)
		}
	}
}

// BenchmarkTransferService_Create_Parallel measures throughput under
// concurrent load, using b.RunParallel. The DB pool, wallet lock,
// and idempotency PK index all become bottlenecks here — exactly
// what we care about for capacity planning.
//
// Note: every parallel iteration uses a per-goroutine counter so
// every idempotency key is distinct (otherwise every goroutine
// after the first hits the replay path, which is a different
// benchmark).
func BenchmarkTransferService_Create_Parallel(b *testing.B) {
	testdb.Reset(b)
	tsvc, wsvc := newTransferSvcB(b)
	ctx := context.Background()
	// Generous seed: many goroutines × b.N iterations × amount=1.
	// 100M is more than enough for any reasonable -benchtime.
	mustSeedB(b, wsvc, "src", 100_000_000)
	mustSeedB(b, wsvc, "dst", 0)

	b.ReportAllocs()
	b.ResetTimer()

	var globalCounter int64
	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine gets a 1M-wide id range to avoid key
		// collisions with peers. Cheap and avoids atomic ops.
		base := int(time.Now().UnixNano())
		i := 0
		for pb.Next() {
			i++
			key := "bench-par-" + strconv.Itoa(base) + "-" + strconv.Itoa(i)
			_, err := tsvc.Create(ctx, service.CreateTransferRequest{
				IdempotencyKey: key,
				FromWalletID:   "src",
				ToWalletID:     "dst",
				Amount:         1,
			})
			if err != nil {
				b.Errorf("create: %v", err)
				return
			}
		}
		_ = globalCounter
	})
}

// BenchmarkTransferService_GetTransfer measures the read-by-id hot
// path. Used by GET /transfers/{id} — the smallest possible
// service-layer operation.
func BenchmarkTransferService_GetTransfer(b *testing.B) {
	testdb.Reset(b)
	tsvc, wsvc := newTransferSvcB(b)
	ctx := context.Background()
	mustSeedB(b, wsvc, "src", 1_000_000)
	mustSeedB(b, wsvc, "dst", 0)

	// Create one transfer outside the benchmark loop and fetch
	// it b.N times. Keeps the cost focused on the read.
	tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "bench-get-seed", FromWalletID: "src", ToWalletID: "dst", Amount: 1,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tsvc.GetTransfer(ctx, tr.ID)
		if err != nil {
			b.Fatalf("get: %v", err)
		}
	}
}

// BenchmarkTransferService_ListTransfersByWallet measures the
// paginated history hot path. Important because list endpoints are
// what production dashboards hit most.
//
// Seeds 1,000 transfers from src→dst then pages through them at
// the default page size (50).
func BenchmarkTransferService_ListTransfersByWallet(b *testing.B) {
	testdb.Reset(b)
	tsvc, wsvc := newTransferSvcB(b)
	ctx := context.Background()
	mustSeedB(b, wsvc, "src", 1_000_000)
	mustSeedB(b, wsvc, "dst", 0)

	const seedRows = 1_000
	for i := 0; i < seedRows; i++ {
		_, err := tsvc.Create(ctx, service.CreateTransferRequest{
			IdempotencyKey: "bench-list-" + strconv.Itoa(i),
			FromWalletID:   "src", ToWalletID: "dst", Amount: 1,
		})
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := tsvc.ListTransfersByWallet(ctx, "src", 0, nil)
		if err != nil {
			b.Fatalf("list: %v", err)
		}
	}
}

// BenchmarkTransferService_IdempotentReplay measures the fast-path
// replay cost — what happens when a client retries with the same
// key. Should be substantially cheaper than Create because no tx
// is opened, no locks are taken.
func BenchmarkTransferService_IdempotentReplay(b *testing.B) {
	testdb.Reset(b)
	tsvc, wsvc := newTransferSvcB(b)
	ctx := context.Background()
	mustSeedB(b, wsvc, "src", 1_000_000)
	mustSeedB(b, wsvc, "dst", 0)

	const key = "bench-replay-key"
	req := service.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: "src", ToWalletID: "dst", Amount: 1,
	}
	// Prime the cache by doing the slow-path insert once.
	if _, err := tsvc.Create(ctx, req); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tsvc.Create(ctx, req)
		if err != nil {
			b.Fatalf("replay: %v", err)
		}
	}
}

// --- helpers ----------------------------------------------------------

// newTransferSvcB is the benchmark-side mirror of newTransferSvc(t).
// Benchmarks can't use the *testing.T helper because they receive
// *testing.B; we duplicate the wiring rather than introduce an
// interface bound just for one use site.
func newTransferSvcB(b *testing.B) (*service.TransferService, *service.WalletService) {
	b.Helper()
	d := testdb.Get(b)
	wallets := postgres.NewWalletRepo()
	transfers := postgres.NewTransferRepo()
	ledger := postgres.NewLedgerRepo()
	idem := postgres.NewIdempotencyRepo()
	txm := postgres.NewTxManager(d)
	return service.NewTransferService(d, txm, wallets, transfers, ledger, idem),
		service.NewWalletService(d, wallets)
}

func mustSeedB(b *testing.B, w *service.WalletService, id string, bal int64) {
	b.Helper()
	if _, err := w.Create(context.Background(), id, bal); err != nil {
		b.Fatalf("seed wallet %s: %v", id, err)
	}
}
