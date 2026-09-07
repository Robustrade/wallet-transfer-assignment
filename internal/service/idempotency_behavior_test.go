package service_test

import (
	"context"
	"sync"
	"testing"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/repository/memory"
	"Wallet_Transfer_Service/internal/service"
)

// A retried request against a FAILED transfer must replay the cached
// FAILED result rather than re-evaluating the business rule. This
// matters because a naive implementation might only cache successes,
// silently re-running (and re-failing, or worse, re-attempting) the
// transfer on every retry.
func TestCreateTransfer_IdempotentReplay_OfFailedTransfer(t *testing.T) {
	store := memory.NewStore()
	ts := service.NewTransferService(store)
	ws := service.NewWalletService(store)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 50)
	mustCreateWallet(t, ws, "wallet_2", 0)
	mustCreateWallet(t, ws, "funder", 1000)

	req := service.CreateTransferRequest{
		IdempotencyKey: "fail-then-replay",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, replayed, err := ts.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if replayed {
		t.Fatalf("first call should not be a replay")
	}
	if first.State != domain.TransferFailed {
		t.Fatalf("expected FAILED, got %s", first.State)
	}

	// Top up the source wallet - if the retry re-evaluated the rule
	// instead of replaying, this transfer would now succeed and debit
	// funds, which must not happen.
	if _, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "topup",
		FromWalletID:   "funder",
		ToWalletID:     "wallet_1",
		Amount:         1000,
	}); err != nil {
		t.Fatalf("top up: %v", err)
	}

	second, replayed, err := ts.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !replayed {
		t.Fatalf("expected second call to be a replay")
	}
	if second.TransferID != first.TransferID {
		t.Fatalf("expected same transfer id, got %s and %s", first.TransferID, second.TransferID)
	}
	if second.State != domain.TransferFailed {
		t.Fatalf("replay must preserve FAILED state, got %s", second.State)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	if from.Balance != 1050 {
		t.Fatalf("replay of a FAILED transfer must not debit funds, got balance %d", from.Balance)
	}

	detail, err := ts.GetTransfer(ctx, first.TransferID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if len(detail.Entries) != 0 {
		t.Fatalf("a FAILED transfer must never gain ledger entries via replay, got %d", len(detail.Entries))
	}
}

// Many goroutines race to claim the same idempotency key with different
// request payloads (different amounts). Exactly one payload must win and
// execute; every other caller must be rejected as a conflict rather than
// silently executing its own version or receiving someone else's result
// under a different guise.
func TestCreateTransfer_ConcurrentConflictingPayloads(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 1000)
	mustCreateWallet(t, ws, "wallet_2", 0)

	const attempts = 20
	const key = "conflict-race-key"

	var wg sync.WaitGroup
	results := make([]*service.TransferResult, attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := service.CreateTransferRequest{
				IdempotencyKey: key,
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         int64(10 + i), // distinct payload per goroutine
			}
			for {
				res, _, err := ts.CreateTransfer(ctx, req)
				if err == domain.ErrIdempotencyKeyInUse {
					continue
				}
				results[i], errs[i] = res, err
				return
			}
		}(i)
	}
	wg.Wait()

	var winners, conflicts int
	var winningAmount int64 = -1
	for i, err := range errs {
		switch err {
		case nil:
			winners++
			if winningAmount == -1 {
				winningAmount = results[i].Amount
			} else if results[i].Amount != winningAmount {
				t.Fatalf("two different winning amounts observed: %d and %d", winningAmount, results[i].Amount)
			}
		case domain.ErrIdempotencyKeyConflict:
			conflicts++
		default:
			t.Fatalf("attempt %d: unexpected error %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 winning payload to execute, got %d", winners)
	}
	if conflicts != attempts-1 {
		t.Fatalf("expected %d conflicts, got %d", attempts-1, conflicts)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	if from.Balance != 1000-winningAmount {
		t.Fatalf("expected exactly one debit of the winning amount %d, got balance %d", winningAmount, from.Balance)
	}
}
