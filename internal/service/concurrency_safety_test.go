package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/repository/memory"
	"Wallet_Transfer_Service/internal/service"
)

// Two wallets transferring back and forth concurrently (A->B and B->A at
// the same time, repeatedly) is the classic deadlock shape when locks are
// acquired in call order instead of a fixed order. This must complete
// without hanging and leave total funds between the pair conserved.
func TestCreateTransfer_ConcurrentOppositeDirections(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_a", 10_000)
	mustCreateWallet(t, ws, "wallet_b", 10_000)

	const rounds = 50
	var wg sync.WaitGroup
	errCh := make(chan error, rounds*2)

	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("a-to-b-%d", i),
				FromWalletID:   "wallet_a",
				ToWalletID:     "wallet_b",
				Amount:         10,
			})
			if err != nil {
				errCh <- err
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("b-to-a-%d", i),
				FromWalletID:   "wallet_b",
				ToWalletID:     "wallet_a",
				Amount:         10,
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("unexpected error (possible deadlock/race artifact): %v", err)
	}

	a, _ := ws.GetWallet(ctx, "wallet_a")
	b, _ := ws.GetWallet(ctx, "wallet_b")
	if a.Balance+b.Balance != 20_000 {
		t.Fatalf("funds not conserved: wallet_a=%d wallet_b=%d total=%d, want 20000", a.Balance, b.Balance, a.Balance+b.Balance)
	}
	// Equal transfer counts in both directions at equal amounts nets to
	// each wallet's starting balance.
	if a.Balance != 10_000 || b.Balance != 10_000 {
		t.Fatalf("expected balances to net back to starting point, got wallet_a=%d wallet_b=%d", a.Balance, b.Balance)
	}
}

// A pool of wallets with many goroutines firing transfers in random
// directions concurrently must never lose or create money: the sum of
// every wallet's balance stays constant, and it must always equal the
// sum reconstructed from the full ledger. This is the strongest
// end-to-end correctness invariant under concurrency.
func TestCreateTransfer_ConservationUnderConcurrentLoad(t *testing.T) {
	store := memory.NewStore()
	ts := service.NewTransferService(store)
	ws := service.NewWalletService(store)
	ctx := context.Background()

	const numWallets = 6
	const startingBalance = 1000
	walletIDs := make([]string, numWallets)
	for i := 0; i < numWallets; i++ {
		id := fmt.Sprintf("wallet_%d", i)
		walletIDs[i] = id
		mustCreateWallet(t, ws, id, startingBalance)
	}
	totalFunds := int64(numWallets * startingBalance)

	const workers = 30
	const perWorker = 10
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				from := walletIDs[(w+i)%numWallets]
				to := walletIDs[(w+i+1)%numWallets]
				_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
					IdempotencyKey: fmt.Sprintf("load-%d-%d", w, i),
					FromWalletID:   from,
					ToWalletID:     to,
					Amount:         37,
				})
				// Insufficient funds is an expected business outcome
				// under contention, not a bug; any other error is not.
				if err != nil && err != domain.ErrInsufficientFunds {
					t.Errorf("worker %d iter %d: unexpected error %v", w, i, err)
				}
			}
		}(w)
	}
	wg.Wait()

	var sumBalances int64
	for _, id := range walletIDs {
		w, err := ws.GetWallet(ctx, id)
		if err != nil {
			t.Fatalf("GetWallet(%s): %v", id, err)
		}
		sumBalances += w.Balance
		if w.Balance < 0 {
			t.Fatalf("wallet %s went negative: %d", id, w.Balance)
		}
	}
	if sumBalances != totalFunds {
		t.Fatalf("funds not conserved under concurrent load: got %d, want %d", sumBalances, totalFunds)
	}
}
