package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/shanks/wallet-transfer-assignment/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferConcurrencySafety(t *testing.T) {
	db := setupTestDB(t)
	walletSvc, transferSvc := setupTestServices(db)
	ctx := context.Background()

	// Seed 2 wallets with 10,000.00 USD (1,000,000 cents each)
	initialBalance := int64(1_000_000)
	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "conc_w1", InitialBalance: initialBalance})
	require.NoError(t, err)
	w2, err := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "conc_w2", InitialBalance: initialBalance})
	require.NoError(t, err)

	const numGoroutinesPerDirection = 25
	const transferAmount = int64(1_000) // 10.00 USD (1000 cents)

	var wg sync.WaitGroup
	errs := make(chan error, numGoroutinesPerDirection*2)

	// Goroutines transferring W1 -> W2
	for i := 0; i < numGoroutinesPerDirection; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			input := service.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("conc_w1_to_w2_%d", id),
				FromWalletID:   w1.ID,
				ToWalletID:     w2.ID,
				Amount:         transferAmount,
			}
			_, err := transferSvc.CreateTransfer(context.Background(), input)
			if err != nil {
				errs <- fmt.Errorf("W1->W2 transfer %d failed: %w", id, err)
			}
		}(i)
	}

	// Goroutines transferring W2 -> W1 (reciprocal transfers running simultaneously to stress lock ordering)
	for i := 0; i < numGoroutinesPerDirection; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			input := service.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("conc_w2_to_w1_%d", id),
				FromWalletID:   w2.ID,
				ToWalletID:     w1.ID,
				Amount:         transferAmount,
			}
			_, err := transferSvc.CreateTransfer(context.Background(), input)
			if err != nil {
				errs <- fmt.Errorf("W2->W1 transfer %d failed: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrency execution error: %v", err)
	}

	// Verify wallet balances after 25 debits and 25 credits of equal amount
	w1Final, err := walletSvc.GetWallet(ctx, w1.ID)
	require.NoError(t, err)
	w2Final, err := walletSvc.GetWallet(ctx, w2.ID)
	require.NoError(t, err)

	assert.Equal(t, initialBalance, w1Final.Balance)
	assert.Equal(t, initialBalance, w2Final.Balance)
	assert.Equal(t, initialBalance*2, w1Final.Balance+w2Final.Balance)

	// Ledger Audit Check: Stored wallet balance must equal derived ledger balance
	audit1, err := walletSvc.AuditWalletBalance(ctx, w1.ID)
	require.NoError(t, err)
	assert.True(t, audit1.IsBalanced)

	audit2, err := walletSvc.AuditWalletBalance(ctx, w2.ID)
	require.NoError(t, err)
	assert.True(t, audit2.IsBalanced)
}

func TestConcurrentIdenticalIdempotencyRequests(t *testing.T) {
	db := setupTestDB(t)
	walletSvc, transferSvc := setupTestServices(db)
	ctx := context.Background()

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "idem_conc_w1", InitialBalance: 1000})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletInput{ID: "idem_conc_w2", InitialBalance: 500})

	const numConcurrentCalls = 10
	const sharedKey = "shared_idempotency_key_999"

	var wg sync.WaitGroup
	results := make(chan *service.TransferResult, numConcurrentCalls)
	errs := make(chan error, numConcurrentCalls)

	for i := 0; i < numConcurrentCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			input := service.CreateTransferInput{
				IdempotencyKey: sharedKey,
				FromWalletID:   w1.ID,
				ToWalletID:     w2.ID,
				Amount:         100,
			}
			res, err := transferSvc.CreateTransfer(context.Background(), input)
			if err != nil {
				errs <- err
			} else {
				results <- res
			}
		}()
	}

	wg.Wait()
	close(results)
	close(errs)

	var successfulTransfers int
	var conflictErrors int

	for res := range results {
		if res != nil && res.Transfer != nil && res.Transfer.Status == domain.TransferStatusProcessed {
			successfulTransfers++
		}
	}
	for err := range errs {
		if err == domain.ErrIdempotencyConflict {
			conflictErrors++
		}
	}

	// At least 1 call succeeds and produces the transfer; any concurrent overlapping calls return conflict or replayed response
	assert.GreaterOrEqual(t, successfulTransfers, 1)

	// Balance should decrease by EXACTLY 100
	w1Final, _ := walletSvc.GetWallet(ctx, w1.ID)
	w2Final, _ := walletSvc.GetWallet(ctx, w2.ID)

	assert.Equal(t, int64(900), w1Final.Balance)
	assert.Equal(t, int64(600), w2Final.Balance)
}
