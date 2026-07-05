//go:build integration

package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/service"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/testutil"
)

// TestCreateTransfer_ConcurrentSameIdempotencyKey_SameBody fires the same
// request N times concurrently. The idempotency_records primary key must
// collapse all of them into exactly one transfer, debited exactly once,
// regardless of how many requests raced to create it.
func TestCreateTransfer_ConcurrentSameIdempotencyKey_SameBody(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 1000)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	req := service.CreateTransferRequest{
		IdempotencyKey: "concurrent-same-body",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	const n = 10
	results := make([]domain.Transfer, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results[i], errs[i] = svc.CreateTransfer(ctx, req)
		}(i)
	}
	wg.Wait()

	firstID := results[0].ID
	for i := range results {
		if errs[i] != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, errs[i])
			continue
		}
		if results[i].ID != firstID {
			t.Errorf("goroutine %d: expected shared transfer ID %s, got %s", i, firstID, results[i].ID)
		}
		if results[i].Status != domain.StatusProcessed {
			t.Errorf("goroutine %d: expected status PROCESSED, got %s", i, results[i].Status)
		}
	}

	var transferCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM transfers`, nil, &transferCount)
	if transferCount != 1 {
		t.Errorf("expected exactly 1 transfer row, got %d", transferCount)
	}

	var fromBalance int64
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_1"}, &fromBalance)
	if fromBalance != 900 {
		t.Errorf("expected wallet_1 debited exactly once to 900, got %d", fromBalance)
	}
}

// TestCreateTransfer_ConcurrentSameIdempotencyKey_DifferentBody fires two
// different request bodies under the same idempotencyKey concurrently.
// Exactly one body must "win" the race on the idempotency_records primary
// key: every call sharing the winning body must succeed identically, and
// every call with the other body must be rejected with
// ErrIdempotencyKeyConflict -- never a mix of successes for both bodies,
// and never more than one transfer persisted.
func TestCreateTransfer_ConcurrentSameIdempotencyKey_DifferentBody(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 1000)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	key := "concurrent-different-body"
	reqA := service.CreateTransferRequest{IdempotencyKey: key, FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	reqB := service.CreateTransferRequest{IdempotencyKey: key, FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 200}

	const perBody = 5
	total := perBody * 2
	results := make([]domain.Transfer, total)
	errs := make([]error, total)

	var wg sync.WaitGroup
	wg.Add(total)
	for i := 0; i < perBody; i++ {
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results[i], errs[i] = svc.CreateTransfer(ctx, reqA)
		}(i)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results[perBody+i], errs[perBody+i] = svc.CreateTransfer(ctx, reqB)
		}(i)
	}
	wg.Wait()

	var winners, conflicts int
	for i := range results {
		switch {
		case errs[i] == nil:
			winners++
		case errors.Is(errs[i], domain.ErrIdempotencyKeyConflict):
			conflicts++
		default:
			t.Errorf("result %d: unexpected error: %v", i, errs[i])
		}
	}
	if winners != perBody || conflicts != perBody {
		t.Fatalf("expected exactly %d successes and %d conflicts (all-or-nothing per request body), got %d successes and %d conflicts",
			perBody, perBody, winners, conflicts)
	}

	var transferCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM transfers`, nil, &transferCount)
	if transferCount != 1 {
		t.Errorf("expected exactly 1 transfer to be persisted despite the conflicting bodies, got %d", transferCount)
	}
}

// TestCreateTransfer_ConcurrentDifferentKeys_InsufficientBalanceForBoth
// fires two transfers from the same wallet concurrently, each individually
// affordable but not both together. Exactly one must PROCESS and the other
// must FAIL with insufficient balance; the wallet's final balance must
// reflect exactly one debit, never both and never neither.
func TestCreateTransfer_ConcurrentDifferentKeys_InsufficientBalanceForBoth(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)
	testutil.SeedWallet(t, pool, "wallet_3", 0)

	reqA := service.CreateTransferRequest{IdempotencyKey: "key-a", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	reqB := service.CreateTransferRequest{IdempotencyKey: "key-b", FromWalletID: "wallet_1", ToWalletID: "wallet_3", Amount: 100}

	var resultA, resultB domain.Transfer
	var errA, errB error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resultA, errA = svc.CreateTransfer(ctx, reqA)
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resultB, errB = svc.CreateTransfer(ctx, reqB)
	}()
	wg.Wait()

	if errA != nil {
		t.Fatalf("transfer A: unexpected error: %v", errA)
	}
	if errB != nil {
		t.Fatalf("transfer B: unexpected error: %v", errB)
	}

	processed, failed := 0, 0
	for _, tr := range []domain.Transfer{resultA, resultB} {
		switch tr.Status {
		case domain.StatusProcessed:
			processed++
		case domain.StatusFailed:
			failed++
			if tr.FailureReason == nil || *tr.FailureReason != "insufficient balance" {
				t.Errorf("expected failure reason %q, got %v", "insufficient balance", tr.FailureReason)
			}
		default:
			t.Errorf("unexpected terminal status %s", tr.Status)
		}
	}
	if processed != 1 || failed != 1 {
		t.Fatalf("expected exactly 1 PROCESSED and 1 FAILED, got %d PROCESSED and %d FAILED", processed, failed)
	}

	var fromBalance, toBalance2, toBalance3 int64
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_1"}, &fromBalance)
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_2"}, &toBalance2)
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_3"}, &toBalance3)

	if fromBalance != 0 {
		t.Errorf("expected wallet_1 debited exactly once down to 0, got %d", fromBalance)
	}
	if toBalance2+toBalance3 != 100 {
		t.Errorf("expected exactly 100 credited to whichever destination won, got wallet_2=%d wallet_3=%d", toBalance2, toBalance3)
	}
}

// TestCreateTransfer_CircularChain_NoDeadlock fires three transfers forming
// a cycle (a->b, b->c, c->a) concurrently. WalletRepository.LockForUpdate
// always acquires row locks in ascending wallet-ID order, which is what
// prevents this from deadlocking regardless of dispatch order; this test
// fails by timing out if that guarantee regresses.
func TestCreateTransfer_CircularChain_NoDeadlock(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_a", 100)
	testutil.SeedWallet(t, pool, "wallet_b", 100)
	testutil.SeedWallet(t, pool, "wallet_c", 100)

	requests := []service.CreateTransferRequest{
		{IdempotencyKey: "chain-a-b", FromWalletID: "wallet_a", ToWalletID: "wallet_b", Amount: 50},
		{IdempotencyKey: "chain-b-c", FromWalletID: "wallet_b", ToWalletID: "wallet_c", Amount: 50},
		{IdempotencyKey: "chain-c-a", FromWalletID: "wallet_c", ToWalletID: "wallet_a", Amount: 50},
	}

	results := make([]domain.Transfer, len(requests))
	errs := make([]error, len(requests))

	var wg sync.WaitGroup
	wg.Add(len(requests))
	for i, req := range requests {
		go func(i int, req service.CreateTransferRequest) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results[i], errs[i] = svc.CreateTransfer(ctx, req)
		}(i, req)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for circular transfer chain to complete -- suspected deadlock in wallet locking")
	}

	for i, err := range errs {
		if err != nil {
			t.Errorf("transfer %d: unexpected error: %v", i, err)
			continue
		}
		if results[i].Status != domain.StatusProcessed {
			t.Errorf("transfer %d: expected status PROCESSED, got %s", i, results[i].Status)
		}
	}

	var balanceA, balanceB, balanceC int64
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_a"}, &balanceA)
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_b"}, &balanceB)
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_c"}, &balanceC)

	if balanceA != 100 || balanceB != 100 || balanceC != 100 {
		t.Errorf("expected all balances conserved at 100 each (net zero movement per wallet), got a=%d b=%d c=%d", balanceA, balanceB, balanceC)
	}
}

// TestCreateTransfer_ConcurrentBatch_LedgerReconcilesWithBalances fires a
// mixed batch of transfers concurrently (some affordable, one deliberately
// not) across a small set of wallets, then proves each wallet's stored
// balance exactly reconciles against its own ledger entries -- computed
// independently via SUM(...) over ledger_entries, not by trusting whatever
// internal bookkeeping the service used -- and that the total balance
// across all wallets is conserved.
func TestCreateTransfer_ConcurrentBatch_LedgerReconcilesWithBalances(t *testing.T) {
	svc, pool := newTestService(t)
	wallets := []string{"wallet_1", "wallet_2", "wallet_3", "wallet_4"}
	const initialBalance = int64(500)
	for _, w := range wallets {
		testutil.SeedWallet(t, pool, w, initialBalance)
	}

	requests := []service.CreateTransferRequest{
		{IdempotencyKey: "batch-1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100},
		{IdempotencyKey: "batch-2", FromWalletID: "wallet_2", ToWalletID: "wallet_3", Amount: 200},
		{IdempotencyKey: "batch-3", FromWalletID: "wallet_3", ToWalletID: "wallet_4", Amount: 300},
		{IdempotencyKey: "batch-4", FromWalletID: "wallet_4", ToWalletID: "wallet_1", Amount: 50},
		{IdempotencyKey: "batch-5", FromWalletID: "wallet_1", ToWalletID: "wallet_3", Amount: 10000}, // insufficient balance
	}

	var wg sync.WaitGroup
	wg.Add(len(requests))
	for _, req := range requests {
		go func(req service.CreateTransferRequest) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := svc.CreateTransfer(ctx, req); err != nil {
				t.Errorf("transfer %s: unexpected error: %v", req.IdempotencyKey, err)
			}
		}(req)
	}
	wg.Wait()

	var totalBalance int64
	for _, w := range wallets {
		var balance int64
		mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{w}, &balance)
		totalBalance += balance

		var credited, debited int64
		mustScan(t, pool, `SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE wallet_id = $1 AND type = 'CREDIT'`, []any{w}, &credited)
		mustScan(t, pool, `SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE wallet_id = $1 AND type = 'DEBIT'`, []any{w}, &debited)

		expected := initialBalance + credited - debited
		if balance != expected {
			t.Errorf("wallet %s: balance %d does not reconcile with ledger (initial %d + credited %d - debited %d = %d)",
				w, balance, initialBalance, credited, debited, expected)
		}
	}

	if totalBalance != initialBalance*int64(len(wallets)) {
		t.Errorf("expected total balance conserved at %d, got %d", initialBalance*int64(len(wallets)), totalBalance)
	}

	var debitCount, creditCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM ledger_entries WHERE type = 'DEBIT'`, nil, &debitCount)
	mustScan(t, pool, `SELECT COUNT(*) FROM ledger_entries WHERE type = 'CREDIT'`, nil, &creditCount)
	if debitCount != creditCount {
		t.Errorf("expected equal DEBIT and CREDIT entry counts, got %d debits and %d credits", debitCount, creditCount)
	}
}
