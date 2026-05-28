//go:build integration

package service_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

func newTransferSvc(t *testing.T) (*service.TransferService, *service.WalletService) {
	t.Helper()
	d := testdb.Get(t)
	wallets := postgres.NewWalletRepo()
	transfers := postgres.NewTransferRepo()
	ledger := postgres.NewLedgerRepo()
	idem := postgres.NewIdempotencyRepo()
	txm := postgres.NewTxManager(d)
	return service.NewTransferService(d, txm, wallets, transfers, ledger, idem),
		service.NewWalletService(d, wallets)
}

func seedWallets(t *testing.T, w *service.WalletService, balances map[string]int64) {
	t.Helper()
	for id, bal := range balances {
		if _, err := w.Create(context.Background(), id, bal); err != nil {
			t.Fatalf("seed wallet %s: %v", id, err)
		}
	}
}

// TestCreateTransfer_HappyPath is the canonical end-to-end check:
// two wallets are seeded, one transfer of 150 moves between them,
// and the resulting balances reflect the debit + credit. This is
// the assignment's primary requirement boiled down to a single
// test function.
func TestCreateTransfer_HappyPath(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"w1": 500, "w2": 100})

	tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k1",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         150,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tr.State != domain.StateProcessed {
		t.Fatalf("state: got %s want PROCESSED", tr.State)
	}

	src, _ := wsvc.Get(ctx, "w1")
	dst, _ := wsvc.Get(ctx, "w2")
	if src.Balance != 350 {
		t.Fatalf("src balance: got %d want 350", src.Balance)
	}
	if dst.Balance != 250 {
		t.Fatalf("dst balance: got %d want 250", dst.Balance)
	}
}

// TestCreateTransfer_LedgerEntriesBalance verifies the double-entry
// invariant directly: for any successful transfer the ledger must
// contain exactly two rows (one DEBIT against the source, one
// CREDIT against the destination) with equal amounts. We query the
// ledger table directly rather than going through any service method
// because we are checking persistence, not service behaviour.
func TestCreateTransfer_LedgerEntriesBalance(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"a": 1000, "b": 0})

	tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "b", Amount: 400,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	d := testdb.Get(t)
	rows, err := d.QueryContext(ctx, `SELECT wallet_id, type, amount FROM ledger_entries WHERE transfer_id = $1 ORDER BY type`, tr.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var entries []struct {
		wallet string
		kind   string
		amount int64
	}
	for rows.Next() {
		var e struct {
			wallet string
			kind   string
			amount int64
		}
		if err := rows.Scan(&e.wallet, &e.kind, &e.amount); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("entries: got %d want 2", len(entries))
	}
	// CREDIT row sorts before DEBIT alphabetically.
	if entries[0].kind != "CREDIT" || entries[0].wallet != "b" || entries[0].amount != 400 {
		t.Fatalf("credit row: %+v", entries[0])
	}
	if entries[1].kind != "DEBIT" || entries[1].wallet != "a" || entries[1].amount != 400 {
		t.Fatalf("debit row: %+v", entries[1])
	}
}

// TestCreateTransfer_InsufficientFunds verifies the FAILED-with-
// commit path: a transfer that would drain the source below zero
// must return a FAILED transfer (not an error), with the documented
// failure reason, and the source/destination balances must be
// unchanged. The transfer + idempotency rows are still committed so
// retries return the same FAILED record.
func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"w1": 50, "w2": 0})

	tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 100,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tr.State != domain.StateFailed {
		t.Fatalf("state: got %s want FAILED", tr.State)
	}
	if tr.FailureReason != "insufficient funds" {
		t.Fatalf("reason: %s", tr.FailureReason)
	}
	src, _ := wsvc.Get(ctx, "w1")
	dst, _ := wsvc.Get(ctx, "w2")
	if src.Balance != 50 || dst.Balance != 0 {
		t.Fatalf("balances changed: src=%d dst=%d", src.Balance, dst.Balance)
	}
}

// TestCreateTransfer_IdempotencyReplay verifies the fast-path
// replay: two calls with the same idempotency key and body must
// return the SAME transfer id, and the source wallet must only be
// debited once. This is the assignment's exactly-once semantic.
func TestCreateTransfer_IdempotencyReplay(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"w1": 1000, "w2": 0})

	req := service.CreateTransferRequest{
		IdempotencyKey: "same-key", FromWalletID: "w1", ToWalletID: "w2", Amount: 200,
	}
	first, err := tsvc.Create(ctx, req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := tsvc.Create(ctx, req)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay returned different transfer: %s vs %s", first.ID, second.ID)
	}
	src, _ := wsvc.Get(ctx, "w1")
	if src.Balance != 800 {
		t.Fatalf("balance shifted on replay: got %d want 800", src.Balance)
	}
}

// TestCreateTransfer_IdempotencyConflict verifies the conflict
// behaviour: replays with the same key but a different request body
// must be rejected with domain.ErrIdempotencyConflict (which the
// handler maps to 409). This protects against the bug class where
// a client retries with mutated parameters under the same key and
// accidentally accepts a different outcome.
func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"w1": 1000, "w2": 0})

	_, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 100,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err = tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 999, // different body
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

// TestCreateTransfer_Validation enumerates every Validate failure
// mode so a future refactor that breaks one of these checks shows
// up immediately. The cases mirror the rules in
// CreateTransferRequest.Validate.
func TestCreateTransfer_Validation(t *testing.T) {
	testdb.Reset(t)
	tsvc, _ := newTransferSvc(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  service.CreateTransferRequest
		want error
	}{
		{"missing key", service.CreateTransferRequest{FromWalletID: "a", ToWalletID: "b", Amount: 1}, domain.ErrInvalidIdempotencyKey},
		{"zero amount", service.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 0}, domain.ErrInvalidAmount},
		{"negative amount", service.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: -1}, domain.ErrInvalidAmount},
		{"same wallet", service.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "a", Amount: 10}, domain.ErrSameWallet},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tsvc.Create(ctx, c.req)
			if !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
}

// TestCreateTransfer_ConcurrentDebits is the headline concurrency
// test. It launches 20 concurrent transfers of 100 each from a
// wallet with balance 1000. Math: only 10 can succeed; the other
// 10 must be FAILED. The test asserts:
//
//   - source balance never went negative (the schema CHECK plus the
//     service's lock-then-validate flow must both hold under load),
//
//   - total = source + destination is conserved (no money was
//     created or destroyed),
//
//   - exactly 10 succeeded and 10 failed (the lock makes the result
//     deterministic, not probabilistic).
//
// This is the test that would fail if the service ever lost the
// FOR UPDATE lock or did a TOCTOU balance read.
func TestCreateTransfer_ConcurrentDebits(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()

	const (
		startingBalance int64 = 1000
		perTransfer     int64 = 100
		numTransfers          = 20 // 20 * 100 = 2000 attempted, only 10 should succeed
	)
	seedWallets(t, wsvc, map[string]int64{"src": startingBalance, "dst": 0})

	var wg sync.WaitGroup
	var processed, failed int64
	wg.Add(numTransfers)
	for i := 0; i < numTransfers; i++ {
		go func(i int) {
			defer wg.Done()
			tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
				IdempotencyKey: keyFor(i),
				FromWalletID:   "src",
				ToWalletID:     "dst",
				Amount:         perTransfer,
			})
			if err != nil {
				t.Errorf("transfer %d: %v", i, err)
				return
			}
			switch tr.State {
			case domain.StateProcessed:
				atomic.AddInt64(&processed, 1)
			case domain.StateFailed:
				atomic.AddInt64(&failed, 1)
			default:
				t.Errorf("transfer %d unexpected state %s", i, tr.State)
			}
		}(i)
	}
	wg.Wait()

	src, _ := wsvc.Get(ctx, "src")
	dst, _ := wsvc.Get(ctx, "dst")

	if src.Balance < 0 {
		t.Fatalf("source went negative: %d", src.Balance)
	}
	if src.Balance+dst.Balance != startingBalance {
		t.Fatalf("ledger not conserved: src=%d dst=%d want sum=%d", src.Balance, dst.Balance, startingBalance)
	}
	if processed != startingBalance/perTransfer {
		t.Fatalf("processed: got %d want %d", processed, startingBalance/perTransfer)
	}
	if processed+failed != int64(numTransfers) {
		t.Fatalf("counts: processed=%d failed=%d total!=%d", processed, failed, numTransfers)
	}
}

// TestCreateTransfer_ConcurrentSameKey is the second concurrency
// pillar. 25 goroutines fire the same request with the same
// idempotency key simultaneously. The PK on idempotency_records is
// what serialises them: one wins the insert; the other 24 either
// take the fast-path replay or hit the slow-path race-loss branch.
//
// The assertions:
//   - every caller observes the SAME transfer id (no key duplication),
//   - the source wallet was debited EXACTLY ONCE (no double-spend
//     even at 25-way contention).
func TestCreateTransfer_ConcurrentSameKey(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"w1": 1000, "w2": 0})

	const goroutines = 25
	req := service.CreateTransferRequest{
		IdempotencyKey: "shared-key", FromWalletID: "w1", ToWalletID: "w2", Amount: 75,
	}

	var wg sync.WaitGroup
	ids := make(chan string, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			tr, err := tsvc.Create(ctx, req)
			if err != nil {
				t.Errorf("create: %v", err)
				return
			}
			ids <- tr.ID.String()
		}()
	}
	wg.Wait()
	close(ids)

	var seen string
	count := 0
	for id := range ids {
		count++
		if seen == "" {
			seen = id
		} else if id != seen {
			t.Fatalf("different transfer ids observed: %s vs %s", seen, id)
		}
	}
	if count != goroutines {
		t.Fatalf("count: got %d want %d", count, goroutines)
	}
	src, _ := wsvc.Get(ctx, "w1")
	if src.Balance != 925 {
		t.Fatalf("balance: got %d want 925 (debited exactly once)", src.Balance)
	}
}

// TestGetTransfer_Existing covers the read-after-write path on the
// service's GetTransfer accessor — proves the round-trip from
// create-then-fetch yields the same id.
func TestGetTransfer_Existing(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"w1": 500, "w2": 0})
	tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k", FromWalletID: "w1", ToWalletID: "w2", Amount: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tsvc.GetTransfer(ctx, tr.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != tr.ID {
		t.Fatalf("id: %s vs %s", got.ID, tr.ID)
	}
}

// TestGetTransfer_NotFound covers the missing-id path: the lookup
// must surface an error (which the handler maps to 404), never
// return a zero-value transfer.
func TestGetTransfer_NotFound(t *testing.T) {
	testdb.Reset(t)
	tsvc, _ := newTransferSvc(t)
	_, err := tsvc.GetTransfer(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestCreateTransfer_FromWalletMissing exercises the FK-violation
// path when the source wallet does not exist. The database catches
// it via the FK on transfers.from_wallet_id; the test just confirms
// the error makes it back out of the service rather than being
// silently swallowed.
func TestCreateTransfer_FromWalletMissing(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"only-dst": 0})
	_, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k", FromWalletID: "ghost", ToWalletID: "only-dst", Amount: 1,
	})
	if err == nil {
		t.Fatal("expected error for missing source wallet")
	}
}

// TestCreateTransfer_ToWalletMissing is the destination-side
// counterpart to FromWalletMissing — same shape, different FK.
func TestCreateTransfer_ToWalletMissing(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"only-src": 10})
	_, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "k", FromWalletID: "only-src", ToWalletID: "ghost", Amount: 1,
	})
	if err == nil {
		t.Fatal("expected error for missing dest wallet")
	}
}

// TestCreateTransfer_FailedTransferIsReplayable is the key
// idempotency-of-failure test: a request that fails with
// insufficient funds, then retries with the same key, must return
// the original FAILED transfer rather than re-attempting the debit.
// Together with InsufficientFunds this proves that the "commit
// FAILED state" branch isn't just persisting — it's actually
// participating in the idempotency replay.
func TestCreateTransfer_FailedTransferIsReplayable(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"w1": 10, "w2": 0})

	req := service.CreateTransferRequest{
		IdempotencyKey: "k", FromWalletID: "w1", ToWalletID: "w2", Amount: 999,
	}
	first, err := tsvc.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != domain.StateFailed {
		t.Fatalf("state: %s", first.State)
	}
	second, err := tsvc.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay returned different id: %s vs %s", first.ID, second.ID)
	}
	if second.State != domain.StateFailed {
		t.Fatalf("replay state: %s", second.State)
	}
}

// TestWalletService_CreateValidation covers the two early-return
// paths in WalletService.Create: empty id and negative balance.
// Both also have schema-level guards but the service catches them
// first with friendlier errors.
func TestWalletService_CreateValidation(t *testing.T) {
	testdb.Reset(t)
	_, wsvc := newTransferSvc(t)
	ctx := context.Background()

	if _, err := wsvc.Create(ctx, "", 0); err == nil {
		t.Fatal("empty id should fail")
	}
	if _, err := wsvc.Create(ctx, "w", -1); err == nil {
		t.Fatal("negative balance should fail")
	}
}

// TestWalletService_CreateDuplicate verifies that inserting two
// wallets with the same id fails (via the PK on wallets.id). The
// schema is doing the work here — this is a regression test against
// somebody adding a CREATE-OR-IGNORE shortcut to the service.
func TestWalletService_CreateDuplicate(t *testing.T) {
	testdb.Reset(t)
	_, wsvc := newTransferSvc(t)
	ctx := context.Background()
	if _, err := wsvc.Create(ctx, "dup", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := wsvc.Create(ctx, "dup", 2); err == nil {
		t.Fatal("expected unique violation")
	}
}

func keyFor(i int) string {
	return "k-" + strconv.Itoa(i)
}
