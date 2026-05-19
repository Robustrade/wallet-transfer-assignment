package service_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/service"
)

func callCtx() (context.Context, context.CancelFunc) {
	// Bounds each call so a hypothetical deadlock fails the test instead of
	// hanging the suite. With sorted dual FOR UPDATE this never trips.
	return context.WithTimeout(context.Background(), 60*time.Second)
}

func TestConcurrent_SameWallets_NoDoubleSpend(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	const n = 10
	from := seedWallet(t, pool, "1000.00")
	to := seedWallet(t, pool, "0.00")

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := callCtx()
			defer cancel()
			_, _, errs[i] = svc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: newID("k"), FromWalletID: from, ToWalletID: to,
				Amount: decimal.RequireFromString("10.00"),
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("transfer %d errored: %v", i, err)
		}
	}
	fromBal := walletBal(t, pool, from)
	toBal := walletBal(t, pool, to)
	mustEqual(t, fromBal, decimal.RequireFromString("900.00"), "from balance")
	mustEqual(t, toBal, decimal.RequireFromString("100.00"), "to balance")
	// Conservation.
	mustEqual(t, fromBal.Add(toBal), decimal.RequireFromString("1000.00"), "total conserved")
	if fromBal.Sign() < 0 {
		t.Fatalf("from balance negative: %s", fromBal)
	}
	// Ledger <-> balance invariant.
	mustEqual(t, signedLedgerSum(t, pool, from), decimal.RequireFromString("-100.00"), "from signed ledger")
	mustEqual(t, signedLedgerSum(t, pool, to), decimal.RequireFromString("100.00"), "to signed ledger")
}

func TestConcurrent_Oversubscribed_NoOverspend(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	const n = 10 // 10 x 10.00 = 100.00 demanded against 50.00 available
	from := seedWallet(t, pool, "50.00")
	to := seedWallet(t, pool, "0.00")

	var wg sync.WaitGroup
	var processed, failed atomic.Int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := callCtx()
			defer cancel()
			tr, _, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: newID("k"), FromWalletID: from, ToWalletID: to,
				Amount: decimal.RequireFromString("10.00"),
			})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			switch tr.Status {
			case domain.TransferProcessed:
				processed.Add(1)
			case domain.TransferFailed:
				failed.Add(1)
			}
		}()
	}
	wg.Wait()

	if processed.Load() != 5 || failed.Load() != 5 {
		t.Fatalf("processed=%d failed=%d, want 5/5", processed.Load(), failed.Load())
	}
	fromBal := walletBal(t, pool, from)
	toBal := walletBal(t, pool, to)
	if fromBal.Sign() < 0 {
		t.Fatalf("source overspent (negative): %s", fromBal)
	}
	mustEqual(t, fromBal, decimal.RequireFromString("0.00"), "from balance")
	mustEqual(t, toBal, decimal.RequireFromString("50.00"), "to balance")
	mustEqual(t, fromBal.Add(toBal), decimal.RequireFromString("50.00"), "total conserved")
}

func TestConcurrent_Bidirectional_NoDeadlock(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	const n = 10 // 5 each way, 10.00 each -> net zero
	a := seedWallet(t, pool, "500.00")
	b := seedWallet(t, pool, "500.00")

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			from, to := a, b
			if i%2 == 1 {
				from, to = b, a
			}
			ctx, cancel := callCtx()
			defer cancel()
			_, _, errs[i] = svc.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: newID("k"), FromWalletID: from, ToWalletID: to,
				Amount: decimal.RequireFromString("10.00"),
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("opposing transfer %d errored (possible deadlock): %v", i, err)
		}
	}
	aBal := walletBal(t, pool, a)
	bBal := walletBal(t, pool, b)
	mustEqual(t, aBal.Add(bBal), decimal.RequireFromString("1000.00"), "total conserved")
	mustEqual(t, aBal, decimal.RequireFromString("500.00"), "a balance net-zero")
	mustEqual(t, bBal, decimal.RequireFromString("500.00"), "b balance net-zero")
}

func TestConcurrent_SameIdempotencyKey_ExactlyOnce(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	const m = 10
	from := seedWallet(t, pool, "100.00")
	to := seedWallet(t, pool, "0.00")
	key := newID("k")
	in := service.CreateTransferInput{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("20.00"),
	}

	var wg sync.WaitGroup
	ids := make([]string, m)
	var createdCount atomic.Int64
	for i := 0; i < m; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := callCtx()
			defer cancel()
			tr, created, err := svc.CreateTransfer(ctx, in)
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			ids[i] = tr.ID
			if created {
				createdCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	distinct := map[string]struct{}{}
	for _, id := range ids {
		distinct[id] = struct{}{}
	}
	if len(distinct) != 1 {
		t.Fatalf("got %d distinct transfer ids, want exactly 1: %v", len(distinct), distinct)
	}
	if createdCount.Load() != 1 {
		t.Fatalf("created=true count = %d, want exactly 1", createdCount.Load())
	}

	var rows int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM transfers WHERE idempotency_key = $1`, key).Scan(&rows); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if rows != 1 {
		t.Fatalf("transfer rows for key = %d, want 1", rows)
	}

	// Side effects applied exactly once.
	mustEqual(t, walletBal(t, pool, from), decimal.RequireFromString("80.00"), "from balance")
	mustEqual(t, walletBal(t, pool, to), decimal.RequireFromString("20.00"), "to balance")
	var id string
	for k := range distinct {
		id = k
	}
	if n := ledgerCount(t, pool, id); n != 2 {
		t.Fatalf("ledger entries = %d, want exactly 2", n)
	}
}
