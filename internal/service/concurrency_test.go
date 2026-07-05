package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/service"
)

// TestExecuteTransfer_ConcurrentTransfersSameWallet fires many concurrent
// transfers debiting the same wallet and asserts:
//   - the final balance is exactly what it should be (no lost updates)
//   - the wallet never goes negative
//   - every transfer either PROCESSED or FAILED cleanly, no partial state
func TestExecuteTransfer_ConcurrentTransfersSameWallet(t *testing.T) {
	svc, conn := newTestService(t)
	resetTables(t, conn)
	ctx := context.Background()

	const (
		numTransfers = 20
		amountEach   = 3000
		startBalance = 100000 // seeded balance for walletA
	)

	var wg sync.WaitGroup
	errsCh := make(chan error, numTransfers)
	resultsCh := make(chan *service.TransferResult, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			result, err := svc.ExecuteTransfer(ctx, service.TransferInput{
				IdempotencyKey: fmt.Sprintf("concurrent-key-%d", i),
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         amountEach,
			})
			if err != nil {
				errsCh <- err
				return
			}
			resultsCh <- result
		}(i)
	}

	wg.Wait()
	close(errsCh)
	close(resultsCh)

	for err := range errsCh {
		t.Errorf("unexpected error from concurrent transfer: %v", err)
	}

	processedCount := 0
	for result := range resultsCh {
		if result.Status == "PROCESSED" {
			processedCount++
		}
	}

	if processedCount != numTransfers {
		t.Fatalf("expected all %d transfers to process successfully, got %d", numTransfers, processedCount)
	}

	var finalFromBalance, finalToBalance int64
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletA).Scan(&finalFromBalance); err != nil {
		t.Fatalf("query from wallet: %v", err)
	}
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletB).Scan(&finalToBalance); err != nil {
		t.Fatalf("query to wallet: %v", err)
	}

	expectedFromBalance := int64(startBalance - numTransfers*amountEach)
	expectedToBalance := int64(50000 + numTransfers*amountEach)

	if finalFromBalance != expectedFromBalance {
		t.Errorf("expected from-wallet balance %d, got %d (lost update or race condition)", expectedFromBalance, finalFromBalance)
	}
	if finalToBalance != expectedToBalance {
		t.Errorf("expected to-wallet balance %d, got %d (lost update or race condition)", expectedToBalance, finalToBalance)
	}
	if finalFromBalance < 0 {
		t.Errorf("wallet balance went negative: %d", finalFromBalance)
	}

	// Every transfer should have produced exactly 2 ledger entries.
	var totalLedgerEntries int
	if err := conn.QueryRow(`SELECT count(*) FROM ledger_entries`).Scan(&totalLedgerEntries); err != nil {
		t.Fatalf("query ledger entries: %v", err)
	}
	if totalLedgerEntries != numTransfers*2 {
		t.Errorf("expected %d ledger entries, got %d", numTransfers*2, totalLedgerEntries)
	}
}

// TestExecuteTransfer_ConcurrentTransfersExceedingBalance verifies that when
// concurrent transfers collectively exceed the wallet's balance, some
// succeed and some fail cleanly — but the wallet never goes negative and
// no money is double-spent.
func TestExecuteTransfer_ConcurrentTransfersExceedingBalance(t *testing.T) {
	svc, conn := newTestService(t)
	resetTables(t, conn)
	ctx := context.Background()

	// Seeded balance is 100000. Each transfer takes 40000.
	// Only 2 of these 5 can possibly succeed.
	const (
		numTransfers = 5
		amountEach   = 40000
	)

	var wg sync.WaitGroup
	resultsCh := make(chan *service.TransferResult, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := svc.ExecuteTransfer(ctx, service.TransferInput{
				IdempotencyKey: fmt.Sprintf("exceed-key-%d", i),
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         amountEach,
			})
			if err != nil {
				t.Errorf("unexpected error (should resolve to FAILED, not error): %v", err)
				return
			}
			resultsCh <- result
		}(i)
	}

	wg.Wait()
	close(resultsCh)

	processed, failed := 0, 0
	for result := range resultsCh {
		switch result.Status {
		case "PROCESSED":
			processed++
		case "FAILED":
			failed++
		}
	}

	if processed != 2 {
		t.Errorf("expected exactly 2 transfers to process (100000/40000), got %d", processed)
	}
	if failed != 3 {
		t.Errorf("expected exactly 3 transfers to fail, got %d", failed)
	}

	var finalBalance int64
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletA).Scan(&finalBalance); err != nil {
		t.Fatalf("query wallet balance: %v", err)
	}
	if finalBalance < 0 {
		t.Fatalf("wallet balance went negative: %d", finalBalance)
	}
	if finalBalance != 20000 {
		t.Errorf("expected final balance 20000 (100000 - 2*40000), got %d", finalBalance)
	}
}
