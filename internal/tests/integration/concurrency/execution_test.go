package concurrency

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/tests/integration/shared"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	cleanup, err := shared.SetupTestEnvironment()
	if err != nil {
		panic(err)
	}
	defer cleanup()

	m.Run()
}

func TestConcurrency_TableDriven(t *testing.T) {
	testCases := GetConcurrencyTestCases()

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s: %s", tc.ID, tc.Name), func(t *testing.T) {
			shared.ClearDatabase()

			sourceIDs, destIDs := setupWallets(tc)

			// Pre-issue all unique keys
			for i := 0; i < tc.NumRoutines; i++ {
				shared.PreIssueKey(fmt.Sprintf("550e8400-e29b-41d4-a716-44665545%04d", i))
			}

			var wg sync.WaitGroup
			results := make(chan int, tc.NumRoutines)

			for i := 0; i < tc.NumRoutines; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					key := fmt.Sprintf("550e8400-e29b-41d4-a716-44665545%04d", idx)
					payload := map[string]interface{}{
						"fromWalletId": sourceIDs[idx],
						"toWalletId":   destIDs[idx],
						"amount":       tc.AmountPerTx,
					}

					resp := shared.ExecuteTransferRequest(payload, key)
					results <- resp.Code
				}(i)
			}

			wg.Wait()
			close(results)

			actualStatusCounts := make(map[int]int)
			for code := range results {
				actualStatusCounts[code]++
			}

			for expectedCode, expectedCount := range tc.ExpectedStatus {
				assert.Equal(t, expectedCount, actualStatusCounts[expectedCode],
					"Status count mismatch for code %d", expectedCode)
			}

			if tc.SourceWalletID == "MULTI_SOURCE" {
				var w2 models.Wallet
				shared.TestDB.First(&w2, "id = ?", tc.DestWalletID)
				assert.Equal(t, tc.FinalW2Balance, w2.Balance)
			}
		})
	}
}

func TestConcurrency_Idempotency_ConcurrentHit(t *testing.T) {
	shared.ClearDatabase()
	w1 := "550e8400-e29b-41d4-a716-446655440001"
	w2 := "550e8400-e29b-41d4-a716-446655440002"
	shared.SeedWallet(w1, 1000)
	shared.SeedWallet(w2, 0)
	key := "550e8400-e29b-41d4-a716-446655440025"
	shared.PreIssueKey(key)

	payload := map[string]interface{}{
		"fromWalletId": w1,
		"toWalletId":   w2,
		"amount":       100,
	}

	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan int, 2)

	// Fire 2 identical requests simultaneously
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			resp := shared.ExecuteTransferRequest(payload, key)
			results <- resp.Code
		}()
	}

	wg.Wait()
	close(results)

	codes := []int{}
	for code := range results {
		codes = append(codes, code)
	}

	// ASSERTION: One should be 200, one should be 409 Conflict
	assert.Contains(t, codes, http.StatusOK)
	assert.Contains(t, codes, http.StatusConflict)
}

func setupWallets(tc ConcurrencyTestCase) (sourceIDs, destIDs []string) {
	switch tc.SourceWalletID {
	case "MULTI_SOURCE":
		for i := 0; i < tc.NumRoutines; i++ {
			id := fmt.Sprintf("550e8400-e29b-41d4-a716-44665546%04d", i)
			shared.SeedWallet(id, tc.InitialBalance)
			sourceIDs = append(sourceIDs, id)
			destIDs = append(destIDs, tc.DestWalletID)
		}
		shared.SeedWallet(tc.DestWalletID, 0)
	case "PARALLEL":
		// Pair 1: A -> B
		wA := "550e8400-e29b-41d4-a716-446655470001"
		wB := "550e8400-e29b-41d4-a716-446655470002"
		// Pair 2: C -> D
		wC := "550e8400-e29b-41d4-a716-446655470003"
		wD := "550e8400-e29b-41d4-a716-446655470004"
		shared.SeedWallet(wA, tc.InitialBalance)
		shared.SeedWallet(wB, 0)
		shared.SeedWallet(wC, tc.InitialBalance)
		shared.SeedWallet(wD, 0)
		sourceIDs = []string{wA, wC}
		destIDs = []string{wB, wD}
	case "DEADLOCK":
		w1 := "550e8400-e29b-41d4-a716-446655480001"
		w2 := "550e8400-e29b-41d4-a716-446655480002"
		shared.SeedWallet(w1, tc.InitialBalance)
		shared.SeedWallet(w2, tc.InitialBalance)
		sourceIDs = []string{w1, w2}
		destIDs = []string{w2, w1}
	default:
		shared.SeedWallet(tc.SourceWalletID, tc.InitialBalance)
		shared.SeedWallet(tc.DestWalletID, 0)
		for i := 0; i < tc.NumRoutines; i++ {
			sourceIDs = append(sourceIDs, tc.SourceWalletID)
			destIDs = append(destIDs, tc.DestWalletID)
		}
	}
	return sourceIDs, destIDs
}
