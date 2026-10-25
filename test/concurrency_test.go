package test_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository/memory"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/service"
)

func TestHighConcurrency_CircularTransfersStress(t *testing.T) {
	repo := memory.NewMemoryRepository()
	txService := service.NewTransferService(repo)
	walletService := service.NewWalletService(repo)
	ctx := context.Background()

	// Setup 4 wallets with 10,000 cents each = total initial capital of 40,000 cents
	walletIDs := []string{"w_alpha", "w_beta", "w_gamma", "w_delta"}
	initialPerWallet := int64(10000)
	totalInitialCapital := initialPerWallet * int64(len(walletIDs))

	for _, id := range walletIDs {
		_, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{
			ID:             id,
			InitialBalance: initialPerWallet,
			Currency:       "USD",
		})
		if err != nil {
			t.Fatalf("failed to create wallet %s: %v", id, err)
		}
	}

	numGoroutines := 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var successfulTransfers int64
	var failedTransfers int64

	// Concurrently dispatch 50 transfers in circular and bidirectional directions
	for i := 0; i < numGoroutines; i++ {
		workerID := i
		go func() {
			defer wg.Done()

			// Create pairs that include bidirectional transfers (A -> B and B -> A)
			fromIdx := workerID % len(walletIDs)
			toIdx := (workerID + 1) % len(walletIDs)

			// Alternating directions to test bidirectional deadlock freedom
			if workerID%2 == 1 {
				fromIdx, toIdx = toIdx, fromIdx
			}

			fromID := walletIDs[fromIdx]
			toID := walletIDs[toIdx]

			req := service.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrency-key-%d", workerID),
				FromWalletID:   fromID,
				ToWalletID:     toID,
				Amount:         100, // Transfer 100 cents
			}

			_, status, err := txService.ExecuteTransfer(ctx, req)
			if err == nil && status == 201 {
				atomic.AddInt64(&successfulTransfers, 1)
			} else {
				atomic.AddInt64(&failedTransfers, 1)
			}
		}()
	}

	// Wait for all 50 concurrent goroutines to finish
	wg.Wait()

	t.Logf("Concurrency test completed: %d successful, %d failed", successfulTransfers, failedTransfers)

	// Invariant 1: Total money must be conserved across the system
	var totalFinalCapital int64
	for _, id := range walletIDs {
		w, err := walletService.GetWallet(ctx, id)
		if err != nil {
			t.Fatalf("failed to get wallet %s: %v", id, err)
		}
		if w.Balance < 0 {
			t.Fatalf("CRITICAL CONCURRENCY FAILURE: wallet %s has negative balance %d", id, w.Balance)
		}
		totalFinalCapital += w.Balance
	}

	if totalFinalCapital != totalInitialCapital {
		t.Fatalf("CRITICAL INVARIANT VIOLATION: expected total funds %d, got %d (difference: %d)",
			totalInitialCapital, totalFinalCapital, totalFinalCapital-totalInitialCapital)
	}

	// Invariant 2: Double-Entry Ledger must balance perfectly across all wallets
	var totalLedgerCredits int64
	var totalLedgerDebits int64

	for _, id := range walletIDs {
		entries, err := repo.GetLedgerEntriesByWalletID(ctx, id)
		if err != nil {
			t.Fatalf("failed to query ledger entries: %v", err)
		}
		for _, e := range entries {
			if e.EntryType == "CREDIT" {
				totalLedgerCredits += e.Amount
			} else if e.EntryType == "DEBIT" {
				totalLedgerDebits += e.Amount
			}
		}
	}

	if totalLedgerCredits != totalLedgerDebits {
		t.Fatalf("LEDGER UNBALANCED: total credits %d != total debits %d", totalLedgerCredits, totalLedgerDebits)
	}
}
