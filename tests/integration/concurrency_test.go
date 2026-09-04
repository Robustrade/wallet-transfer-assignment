package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrentDebits_DoubleSpendPrevention(t *testing.T) {
	ts := SetupTestSuite(t)
	if ts == nil {
		return
	}
	defer ts.DB.Close()

	// Initial balance: $100.00
	wA := CreateTestWallet(t, ts, "100.00")
	wB := CreateTestWallet(t, ts, "100.00")

	numConcurrentRequests := 10
	transferAmount := "80.00"

	var wg sync.WaitGroup
	results := make(chan int, numConcurrentRequests)

	for i := 0; i < numConcurrentRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			reqBody := service.CreateTransferInput{
				FromWalletID: wA.ID.String(),
				ToWalletID:   wB.ID.String(),
				Amount:       transferAmount,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			wRecorder := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			ts.Router.ServeHTTP(wRecorder, req)
			results <- wRecorder.Code
		}()
	}

	wg.Wait()
	close(results)

	successCount := 0
	insufficientFundsCount := 0

	for code := range results {
		if code == http.StatusCreated {
			successCount++
		} else if code == http.StatusUnprocessableEntity {
			insufficientFundsCount++
		}
	}

	// Verify exact concurrency safety expectations
	assert.Equal(t, 1, successCount, "Exactly one transfer of $80 should succeed out of $100 total balance")
	assert.Equal(t, 9, insufficientFundsCount, "Remaining 9 concurrent requests must fail with insufficient funds")

	// Check final balances in DB
	ctx := context.Background()
	finalWA, err := ts.WalletService.GetWalletBalance(ctx, wA.ID)
	require.NoError(t, err)
	assert.Equal(t, "20.00", finalWA.Balance.String())

	finalWB, err := ts.WalletService.GetWalletBalance(ctx, wB.ID)
	require.NoError(t, err)
	assert.Equal(t, "180.00", finalWB.Balance.String())

	// Verify ledger entries balance
	entriesWA, err := ts.WalletService.GetWalletTransactions(ctx, wA.ID)
	require.NoError(t, err)
	require.Len(t, entriesWA, 1)
	assert.Equal(t, domain.LedgerTypeDebit, entriesWA[0].Type)
	assert.Equal(t, "80.00", entriesWA[0].Amount.String())
}

func TestConcurrentCrossTransfers_DeadlockPrevention(t *testing.T) {
	ts := SetupTestSuite(t)
	if ts == nil {
		return
	}
	defer ts.DB.Close()

	wA := CreateTestWallet(t, ts, "1000.00")
	wB := CreateTestWallet(t, ts, "1000.00")

	numRequestsPerDirection := 10
	transferAmount := "10.00"

	var wg sync.WaitGroup

	// Direction 1: A -> B
	for i := 0; i < numRequestsPerDirection; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reqBody := service.CreateTransferInput{
				FromWalletID: wA.ID.String(),
				ToWalletID:   wB.ID.String(),
				Amount:       transferAmount,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			wRecorder := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			ts.Router.ServeHTTP(wRecorder, req)
			assert.Equal(t, http.StatusCreated, wRecorder.Code)
		}()
	}

	// Direction 2: B -> A (Opposite direction concurrently)
	for i := 0; i < numRequestsPerDirection; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reqBody := service.CreateTransferInput{
				FromWalletID: wB.ID.String(),
				ToWalletID:   wA.ID.String(),
				Amount:       transferAmount,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			wRecorder := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			ts.Router.ServeHTTP(wRecorder, req)
			assert.Equal(t, http.StatusCreated, wRecorder.Code)
		}()
	}

	wg.Wait()

	// Verify all 20 transfers completed without deadlocking
	ctx := context.Background()
	finalWA, err := ts.WalletService.GetWalletBalance(ctx, wA.ID)
	require.NoError(t, err)
	assert.Equal(t, "1000.00", finalWA.Balance.String())

	finalWB, err := ts.WalletService.GetWalletBalance(ctx, wB.ID)
	require.NoError(t, err)
	assert.Equal(t, "1000.00", finalWB.Balance.String())
}
