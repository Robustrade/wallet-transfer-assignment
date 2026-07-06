package repository

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"wallet-transfer/internal/domain"

	_ "github.com/lib/pq"
)

// setupTestDB connects to the local Docker Postgres and resets the test wallets
func setupTestDB(t *testing.T) *sql.DB {
	dsn := "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to connect to test DB: %v", err)
	}

	// Reset the balances for test wallets to exactly 1000 and 500
	_, err = db.Exec(`
		UPDATE wallets SET balance = 1000 WHERE id = 'wallet_1';
		UPDATE wallets SET balance = 500 WHERE id = 'wallet_2';
		DELETE FROM ledger_entries;
		DELETE FROM transfers;
		DELETE FROM idempotency_records;
	`)
	if err != nil {
		t.Fatalf("Failed to seed test DB: %v", err)
	}

	return db
}

func TestExecuteTransferTx_Concurrency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewTransferRepository(db)
	ctx := context.Background()

	// Scenario: wallet_1 has $1000.
	// We will try to fire 10 concurrent requests of $100 each using the EXACT SAME idempotency key.
	// Only ONE should succeed. The other 9 should be blocked and return the cached response.

	req := domain.TransferRequest{
		IdempotencyKey: "concurrent-test-key-1",
		FromWalletId:   "wallet_1",
		ToWalletId:     "wallet_2",
		Amount:         100.00,
	}
	reqHash := "dummy-hash"

	var wg sync.WaitGroup
	var successCount int
	var mu sync.Mutex

	// Fire 10 concurrent goroutines
	numRequests := 10
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()

			resp, err := repo.ExecuteTransferTx(ctx, req, reqHash)

			mu.Lock()
			defer mu.Unlock()

			// If it returned a processed status, count it
			if err == nil && resp != nil && resp.Status == domain.TransferStatusProcessed {
				successCount++
			}
		}()
	}

	// Wait for all 10 requests to finish fighting each other in Postgres
	wg.Wait()

	// 1. ASSERT IDEMPOTENCY: Only exactly ONE transaction should have successfully processed
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful transfer, got %d", successCount)
	}

	// 2. ASSERT BALANCES: wallet_1 should have exactly 900 left, wallet_2 should have 600
	var w1Balance, w2Balance float64
	db.QueryRow("SELECT balance FROM wallets WHERE id = 'wallet_1'").Scan(&w1Balance)
	db.QueryRow("SELECT balance FROM wallets WHERE id = 'wallet_2'").Scan(&w2Balance)

	if w1Balance != 900.00 {
		t.Errorf("Expected wallet_1 balance to be 900, got %f", w1Balance)
	}
	if w2Balance != 600.00 {
		t.Errorf("Expected wallet_2 balance to be 600, got %f", w2Balance)
	}
}
