package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
)

// TestCreateTransfer_ConcurrentDebits fires many concurrent transfers out of
// the same wallet and asserts the final balance and ledger are exactly what
// a serial execution would produce, with no lost updates or double
// spending. The PostgreSQL-specific locking behavior is covered by the
// integration tests in internal/repository/postgres.
func TestCreateTransfer_ConcurrentDebits(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()

	const startingBalance = 10_000
	const transferAmount = 10
	const numTransfers = 200 // 200 * 10 = 2000, well within the starting balance

	seedWallet(t, wallets, "wallet_1", startingBalance)
	seedWallet(t, wallets, "wallet_2", 0)

	var wg sync.WaitGroup
	errs := make([]error, numTransfers)
	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         transferAmount,
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("transfer %d failed: %v", i, err)
		}
	}

	from, err := wallets.GetWallet(ctx, "wallet_1")
	if err != nil {
		t.Fatalf("GetWallet(wallet_1): %v", err)
	}
	to, err := wallets.GetWallet(ctx, "wallet_2")
	if err != nil {
		t.Fatalf("GetWallet(wallet_2): %v", err)
	}

	wantFrom := int64(startingBalance - numTransfers*transferAmount)
	wantTo := int64(numTransfers * transferAmount)
	if from.Balance != wantFrom {
		t.Errorf("wallet_1 balance = %d, want %d (lost update under concurrency)", from.Balance, wantFrom)
	}
	if to.Balance != wantTo {
		t.Errorf("wallet_2 balance = %d, want %d (lost update under concurrency)", to.Balance, wantTo)
	}

	history, err := wallets.ListTransferHistory(ctx, "wallet_1")
	if err != nil {
		t.Fatalf("ListTransferHistory: %v", err)
	}
	if len(history) != numTransfers {
		t.Fatalf("got %d transfers recorded, want %d", len(history), numTransfers)
	}
	for _, xfer := range history {
		if xfer.State != domain.TransferProcessed {
			t.Errorf("transfer %s state = %s, want PROCESSED", xfer.ID, xfer.State)
		}
	}
}

// TestCreateTransfer_ConcurrentDuplicateRequests fires the same
// idempotencyKey concurrently and asserts exactly one transfer is created
// and the wallet is only debited once, proving duplicate concurrent
// delivery of the same request cannot cause a double spend.
func TestCreateTransfer_ConcurrentDuplicateRequests(t *testing.T) {
	transfers, wallets := newTestServices(t)
	ctx := context.Background()
	seedWallet(t, wallets, "wallet_1", 1000)
	seedWallet(t, wallets, "wallet_2", 0)

	const numDuplicates = 50
	var wg sync.WaitGroup
	ids := make([]string, numDuplicates)
	errs := make([]error, numDuplicates)
	for i := 0; i < numDuplicates; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: "same-key-concurrent",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         100,
			})
			errs[i] = err
			if err == nil {
				ids[i] = result.Transfer.ID
			}
		}(i)
	}
	wg.Wait()

	firstID := ""
	for i, err := range errs {
		// A concurrent duplicate may legitimately observe "in progress" if
		// it arrives before the original commits; that is a valid,
		// retry-able outcome, not a bug.
		if err != nil && !isRetryable(err) {
			t.Fatalf("request %d failed unexpectedly: %v", i, err)
		}
		if err == nil {
			if firstID == "" {
				firstID = ids[i]
			} else if ids[i] != firstID {
				t.Fatalf("got two different transfer IDs (%s, %s) for the same idempotencyKey", firstID, ids[i])
			}
		}
	}

	from, _ := wallets.GetWallet(ctx, "wallet_1")
	if from.Balance != 900 {
		t.Errorf("wallet_1 balance = %d, want 900 (duplicate concurrent requests must debit exactly once)", from.Balance)
	}
}

func isRetryable(err error) bool {
	return errors.Is(err, domain.ErrIdempotencyInProgress)
}
