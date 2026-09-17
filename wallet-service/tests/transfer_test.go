package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// =============================================================================
// Happy Path
// =============================================================================

func TestTransfer_HappyPath(t *testing.T) {
	resetDB(t)

	rr, resp := doTransfer(t, transferRequest{
		IdempotencyKey: "happy-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if resp.Status != "PROCESSED" {
		t.Fatalf("expected PROCESSED, got %s", resp.Status)
	}
	if resp.Amount != 100 {
		t.Fatalf("expected amount 100, got %d", resp.Amount)
	}
	if resp.ID == "" {
		t.Fatal("expected non-empty transfer ID")
	}

	// Verify balances
	w1 := getWallet(t, "wallet_1")
	w2 := getWallet(t, "wallet_2")
	if w1.Balance != 9900 {
		t.Fatalf("expected wallet_1 balance 9900, got %d", w1.Balance)
	}
	if w2.Balance != 10100 {
		t.Fatalf("expected wallet_2 balance 10100, got %d", w2.Balance)
	}

	// Verify ledger entries
	count := getLedgerCount(t, resp.ID)
	if count != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", count)
	}
}

// =============================================================================
// Idempotency
// =============================================================================

func TestTransfer_IdempotentReplay(t *testing.T) {
	resetDB(t)

	req := transferRequest{
		IdempotencyKey: "idem-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	}

	// First request
	rr1, resp1 := doTransfer(t, req)
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first request: expected 201, got %d", rr1.Code)
	}

	// Second request (same idempotency key)
	rr2, resp2 := doTransfer(t, req)
	if rr2.Code != http.StatusCreated {
		t.Fatalf("replay request: expected 201, got %d: %s", rr2.Code, rr2.Body.String())
	}

	// Same transfer ID returned
	if resp1.ID != resp2.ID {
		t.Fatalf("expected same transfer ID, got %s and %s", resp1.ID, resp2.ID)
	}

	// Only one transfer in DB
	count := getTransferCount(t, "idem-1")
	if count != 1 {
		t.Fatalf("expected 1 transfer, got %d", count)
	}

	// Balance should reflect only one transfer
	w1 := getWallet(t, "wallet_1")
	if w1.Balance != 9800 {
		t.Fatalf("expected wallet_1 balance 9800, got %d", w1.Balance)
	}

	// Only 2 ledger entries total
	ledgerCount := getLedgerCount(t, resp1.ID)
	if ledgerCount != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", ledgerCount)
	}
}

// =============================================================================
// Insufficient Funds
// =============================================================================

func TestTransfer_InsufficientFunds(t *testing.T) {
	resetDB(t)

	rr, _ := doTransfer(t, transferRequest{
		IdempotencyKey: "insuff-1",
		FromWalletID:   "wallet_3",
		ToWalletID:     "wallet_1",
		Amount:         99999,
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}

	// Balance unchanged
	w3 := getWallet(t, "wallet_3")
	if w3.Balance != 5000 {
		t.Fatalf("expected wallet_3 balance 5000, got %d", w3.Balance)
	}
}

// =============================================================================
// Validation Errors
// =============================================================================

func TestTransfer_MissingIdempotencyKey(t *testing.T) {
	resetDB(t)
	rr, _ := doTransfer(t, transferRequest{
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       100,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestTransfer_ZeroAmount(t *testing.T) {
	resetDB(t)
	rr, _ := doTransfer(t, transferRequest{
		IdempotencyKey: "zero-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         0,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestTransfer_NegativeAmount(t *testing.T) {
	resetDB(t)
	rr, _ := doTransfer(t, transferRequest{
		IdempotencyKey: "neg-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         -100,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestTransfer_SelfTransfer(t *testing.T) {
	resetDB(t)
	rr, _ := doTransfer(t, transferRequest{
		IdempotencyKey: "self-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         100,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestTransfer_NonExistentWallet(t *testing.T) {
	resetDB(t)
	rr, _ := doTransfer(t, transferRequest{
		IdempotencyKey: "noexist-1",
		FromWalletID:   "wallet_999",
		ToWalletID:     "wallet_1",
		Amount:         100,
	})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// =============================================================================
// Ledger Integrity
// =============================================================================

func TestLedger_DebitsEqualCredits(t *testing.T) {
	resetDB(t)

	// Execute several transfers
	transfers := []transferRequest{
		{IdempotencyKey: "ledger-1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 500},
		{IdempotencyKey: "ledger-2", FromWalletID: "wallet_2", ToWalletID: "wallet_3", Amount: 300},
		{IdempotencyKey: "ledger-3", FromWalletID: "wallet_3", ToWalletID: "wallet_1", Amount: 100},
	}

	for _, req := range transfers {
		rr, _ := doTransfer(t, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("transfer %s failed: %d", req.IdempotencyKey, rr.Code)
		}
	}

	// Verify total debits = total credits
	debits := getTotalDebits(t)
	credits := getTotalCredits(t)
	if debits != credits {
		t.Fatalf("ledger imbalance: debits=%d, credits=%d", debits, credits)
	}

	// Verify exactly 6 ledger entries (2 per transfer)
	var totalEntries int
	testDB.QueryRow("SELECT COUNT(*) FROM ledger_entries").Scan(&totalEntries)
	if totalEntries != 6 {
		t.Fatalf("expected 6 ledger entries, got %d", totalEntries)
	}
}

// =============================================================================
// Concurrency Safety
// =============================================================================

func TestTransfer_ConcurrentDebitsFromSameWallet(t *testing.T) {
	resetDB(t)

	// wallet_1 has 10000. Send 10 concurrent transfers of 1000 each.
	// Only 10 should succeed (total = 10000 = full balance).
	concurrency := 15
	amount := int64(1000)

	var wg sync.WaitGroup
	results := make(chan int, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rr, _ := doTransfer(t, transferRequest{
				IdempotencyKey: fmt.Sprintf("conc-%d", idx),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			results <- rr.Code
		}(i)
	}

	wg.Wait()
	close(results)

	successCount := 0
	failCount := 0
	for code := range results {
		if code == http.StatusCreated {
			successCount++
		} else {
			failCount++
		}
	}

	// Verify final balance is non-negative
	w1 := getWallet(t, "wallet_1")
	if w1.Balance < 0 {
		t.Fatalf("wallet_1 balance went negative: %d", w1.Balance)
	}

	// Verify the math adds up
	expectedW1Balance := int64(10000) - int64(successCount)*amount
	if w1.Balance != expectedW1Balance {
		t.Fatalf("wallet_1 balance mismatch: expected %d, got %d (successes=%d)",
			expectedW1Balance, w1.Balance, successCount)
	}

	// Verify wallet_2 received the correct amount
	w2 := getWallet(t, "wallet_2")
	expectedW2Balance := int64(10000) + int64(successCount)*amount
	if w2.Balance != expectedW2Balance {
		t.Fatalf("wallet_2 balance mismatch: expected %d, got %d",
			expectedW2Balance, w2.Balance)
	}

	// Verify ledger still balances
	debits := getTotalDebits(t)
	credits := getTotalCredits(t)
	if debits != credits {
		t.Fatalf("ledger imbalance after concurrent transfers: debits=%d, credits=%d", debits, credits)
	}

	t.Logf("concurrent test: %d succeeded, %d failed, wallet_1=%d, wallet_2=%d",
		successCount, failCount, w1.Balance, w2.Balance)
}

func TestTransfer_ConcurrentIdempotency(t *testing.T) {
	resetDB(t)

	// Send the same transfer 10 times concurrently
	concurrency := 10
	var wg sync.WaitGroup
	transferIDs := make(chan string, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr, resp := doTransfer(t, transferRequest{
				IdempotencyKey: "conc-idem-same",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         500,
			})
			if rr.Code == http.StatusCreated {
				transferIDs <- resp.ID
			}
		}()
	}

	wg.Wait()
	close(transferIDs)

	// All should return the same transfer ID
	var ids []string
	for id := range transferIDs {
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		t.Fatal("expected at least one successful transfer")
	}

	firstID := ids[0]
	for _, id := range ids[1:] {
		if id != firstID {
			t.Fatalf("expected all transfer IDs to match, got %s and %s", firstID, id)
		}
	}

	// Only one transfer in DB
	count := getTransferCount(t, "conc-idem-same")
	if count != 1 {
		t.Fatalf("expected 1 transfer, got %d", count)
	}

	// Balance reflects only one transfer
	w1 := getWallet(t, "wallet_1")
	if w1.Balance != 9500 {
		t.Fatalf("expected wallet_1 balance 9500, got %d", w1.Balance)
	}
}

// =============================================================================
// Health Check
// =============================================================================

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	testRouter.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// =============================================================================
// Wallet Balance API
// =============================================================================

func TestGetWallet(t *testing.T) {
	resetDB(t)

	w := getWallet(t, "wallet_1")
	if w.Balance != 10000 {
		t.Fatalf("expected balance 10000, got %d", w.Balance)
	}
}

func TestGetWallet_NotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/wallets/nonexistent", nil)
	rr := httptest.NewRecorder()
	testRouter.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestTransfer_IdempotentReplayInsufficientFunds(t *testing.T) {
	resetDB(t)

	req := transferRequest{
		IdempotencyKey: "idem-insuff-1",
		FromWalletID:   "wallet_3", // has 5000
		ToWalletID:     "wallet_1",
		Amount:         99999, // insufficient
	}

	// First request
	rr1, _ := doTransfer(t, req)
	if rr1.Code != http.StatusUnprocessableEntity {
		t.Fatalf("first request: expected 422, got %d", rr1.Code)
	}

	// Second request (same idempotency key)
	rr2, _ := doTransfer(t, req)
	if rr2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("replay request: expected 422, got %d: %s", rr2.Code, rr2.Body.String())
	}

	// Verify bodies match (or at least both have FAILED status)
	var body1 map[string]interface{}
	json.Unmarshal(rr1.Body.Bytes(), &body1)
	
	var body2 map[string]interface{}
	json.Unmarshal(rr2.Body.Bytes(), &body2)
	
	t1 := body1["transfer"].(map[string]interface{})
	t2 := body2["transfer"].(map[string]interface{})
	
	if t1["id"] != t2["id"] {
		t.Fatalf("expected same transfer ID, got %v and %v", t1["id"], t2["id"])
	}

	// Only one transfer in DB
	count := getTransferCount(t, "idem-insuff-1")
	if count != 1 {
		t.Fatalf("expected 1 transfer, got %d", count)
	}
}

func TestTransfer_ConcurrentIdempotencyInsufficientFunds(t *testing.T) {
	resetDB(t)

	// Send the same insufficient funds transfer 10 times concurrently
	concurrency := 10
	var wg sync.WaitGroup
	statusCodes := make(chan int, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr, _ := doTransfer(t, transferRequest{
				IdempotencyKey: "conc-idem-insuff",
				FromWalletID:   "wallet_3", // has 5000
				ToWalletID:     "wallet_1",
				Amount:         99999, // insufficient
			})
			statusCodes <- rr.Code
		}()
	}

	wg.Wait()
	close(statusCodes)

	// All should return 422 Unprocessable Entity, none should be 500
	for code := range statusCodes {
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("expected all responses to be 422, got %d", code)
		}
	}

	// Only one transfer in DB
	count := getTransferCount(t, "conc-idem-insuff")
	if count != 1 {
		t.Fatalf("expected 1 transfer, got %d", count)
	}
}
