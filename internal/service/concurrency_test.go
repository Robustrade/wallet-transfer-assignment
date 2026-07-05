package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/service"
)

// TestCreateTransfer_ConcurrentDebitsNeverOverdraw fires more concurrent
// transfers out of one wallet than its balance can possibly cover, and
// asserts the balance never goes negative and exactly as many transfers
// succeed as the balance allows — proving there's no lost-update race
// between the balance check and the debit.
func TestCreateTransfer_ConcurrentDebitsNeverOverdraw(t *testing.T) {
	svc, store := newFixture() // wallet_1 starts at 500

	const attempts = 20
	const amount = 50 // 20 * 50 = 1000, double the available balance

	var wg sync.WaitGroup
	results := make([]domain.TransferState, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", i, err)
				return
			}
			results[i] = res.State
		}(i)
	}
	wg.Wait()

	processed := 0
	for _, s := range results {
		if s == domain.TransferProcessed {
			processed++
		}
	}

	from, _ := store.GetWallet("wallet_1")
	to, _ := store.GetWallet("wallet_2")

	if from.Balance < 0 {
		t.Fatalf("wallet_1 went negative: %d", from.Balance)
	}
	// Exactly 10 of the 20 transfers can succeed (500 / 50) — not fewer
	// (that'd mean over-cautious locking) and not more (that'd mean a race).
	if processed != 10 {
		t.Fatalf("expected exactly 10 processed transfers, got %d", processed)
	}
	if from.Balance != 0 {
		t.Fatalf("expected wallet_1 balance 0, got %d", from.Balance)
	}
	if to.Balance != 600 {
		t.Fatalf("expected wallet_2 balance 600, got %d", to.Balance)
	}

	entries := store.Ledger()
	if len(entries) != processed*2 {
		t.Fatalf("expected %d ledger entries for %d processed transfers, got %d", processed*2, processed, len(entries))
	}
}

// TestCreateTransfer_ConcurrentDuplicateIdempotencyKey fires the *same*
// idempotencyKey from many goroutines at once and checks the wallet is only
// ever debited once, with every caller observing the same transfer.
func TestCreateTransfer_ConcurrentDuplicateIdempotencyKey(t *testing.T) {
	svc, store := newFixture()

	const attempts = 15
	var wg sync.WaitGroup
	transferIDs := make(chan string, attempts)
	errs := make(chan error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
				IdempotencyKey: "shared-key",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         10,
			})
			if err != nil {
				errs <- err
				return
			}
			transferIDs <- res.TransferID
		}()
	}
	wg.Wait()
	close(transferIDs)
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error from a concurrent duplicate request: %v", err)
	}

	seen := map[string]bool{}
	for id := range transferIDs {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("expected exactly 1 distinct transfer id across all %d concurrent duplicates, got %d", attempts, len(seen))
	}

	from, _ := store.GetWallet("wallet_1")
	if from.Balance != 490 {
		t.Fatalf("expected wallet_1 balance 490 (exactly one 10-unit debit applied), got %d", from.Balance)
	}
}
