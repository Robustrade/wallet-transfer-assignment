package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"wallet-transfer/internal/app"
	"wallet-transfer/internal/domain"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

// setupTestRouter connects to the local Docker Postgres, resets the wallets, and returns a fully wired Gin engine.
func setupTestRouter(t *testing.T) (*gin.Engine, *sql.DB) {
	gin.SetMode(gin.TestMode)

	dsn := "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to connect to test DB: %v", err)
	}

	// Reset the database state
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

	// Initialize the app just like main.go does
	application := app.Initialize(db)
	r := gin.Default()
	application.SetupRouter(r)

	return r, db
}

func getBalance(t *testing.T, db *sql.DB, walletID string) float64 {
	var bal float64
	err := db.QueryRow("SELECT balance FROM wallets WHERE id = $1", walletID).Scan(&bal)
	if err != nil {
		t.Fatalf("Failed to get balance for %s: %v", walletID, err)
	}
	return bal
}

func TestTransfer_Success(t *testing.T) {
	r, db := setupTestRouter(t)
	defer db.Close()

	reqPayload := domain.TransferRequest{
		IdempotencyKey: "test-success-key",
		FromWalletId:   "wallet_1",
		ToWalletId:     "wallet_2",
		Amount:         100.00,
	}
	reqBytes, _ := json.Marshal(reqPayload)

	req, _ := http.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Assert 200 OK
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Assert Balances
	w1Bal := getBalance(t, db, "wallet_1")
	w2Bal := getBalance(t, db, "wallet_2")

	if w1Bal != 900.00 {
		t.Errorf("Expected wallet_1 balance to be 900, got %f", w1Bal)
	}
	if w2Bal != 600.00 {
		t.Errorf("Expected wallet_2 balance to be 600, got %f", w2Bal)
	}
}

func TestTransfer_Idempotent(t *testing.T) {
	r, db := setupTestRouter(t)
	defer db.Close()

	reqPayload := domain.TransferRequest{
		IdempotencyKey: "test-idempotent-key",
		FromWalletId:   "wallet_1",
		ToWalletId:     "wallet_2",
		Amount:         250.00,
	}
	reqBytes, _ := json.Marshal(reqPayload)

	// FIRST REQUEST
	req1, _ := http.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewBuffer(reqBytes))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("First request failed: %d, %s", w1.Code, w1.Body.String())
	}

	// SECOND REQUEST (Exact same payload)
	req2, _ := http.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewBuffer(reqBytes))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Second request failed: %d, %s", w2.Code, w2.Body.String())
	}

	// Assert responses match
	if w1.Body.String() != w2.Body.String() {
		t.Errorf("Expected exact same response for idempotent request. \nGot1: %s\nGot2: %s", w1.Body.String(), w2.Body.String())
	}

	// Assert balances only changed ONCE
	w1Bal := getBalance(t, db, "wallet_1")
	w2Bal := getBalance(t, db, "wallet_2")

	if w1Bal != 750.00 {
		t.Errorf("Expected wallet_1 balance to be 750, got %f", w1Bal)
	}
	if w2Bal != 750.00 {
		t.Errorf("Expected wallet_2 balance to be 750, got %f", w2Bal)
	}
}

func TestTransfer_InsufficientFunds(t *testing.T) {
	r, db := setupTestRouter(t)
	defer db.Close()

	reqPayload := domain.TransferRequest{
		IdempotencyKey: "test-insufficient-key",
		FromWalletId:   "wallet_1",
		ToWalletId:     "wallet_2",
		Amount:         5000.00, // wallet_1 only has 1000
	}
	reqBytes, _ := json.Marshal(reqPayload)

	req, _ := http.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Assert 422 Unprocessable Entity
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("Expected status 422, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Assert balances DID NOT change
	w1Bal := getBalance(t, db, "wallet_1")
	w2Bal := getBalance(t, db, "wallet_2")

	if w1Bal != 1000.00 {
		t.Errorf("Expected wallet_1 balance to be 1000, got %f", w1Bal)
	}
	if w2Bal != 500.00 {
		t.Errorf("Expected wallet_2 balance to be 500, got %f", w2Bal)
	}
}

func TestTransfer_WalletNotFound(t *testing.T) {
	r, db := setupTestRouter(t)
	defer db.Close()

	reqPayload := domain.TransferRequest{
		IdempotencyKey: "test-not-found-key",
		FromWalletId:   "wallet_99", // Doesn't exist
		ToWalletId:     "wallet_2",
		Amount:         50.00,
	}
	reqBytes, _ := json.Marshal(reqPayload)

	req, _ := http.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Assert 404 Not Found
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestTransfer_Concurrency_DoubleSpend(t *testing.T) {
	r, db := setupTestRouter(t)
	defer db.Close()

	reqPayload := domain.TransferRequest{
		IdempotencyKey: "test-concurrent-key",
		FromWalletId:   "wallet_1",
		ToWalletId:     "wallet_2",
		Amount:         100.00,
	}
	reqBytes, _ := json.Marshal(reqPayload)

	// We will track how many requests return 200 OK
	var successCount int
	var mu sync.Mutex
	var wg sync.WaitGroup

	numRequests := 10
	wg.Add(numRequests)

	// Fire 10 exact duplicate requests at the exact same time
	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()

			req, _ := http.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewBuffer(reqBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			// If the request succeeded, count it
			if w.Code == http.StatusOK {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	// Wait for all 10 goroutines to finish
	wg.Wait()

	// Assert only ONE request got a 200 OK (or all got 200 OK but the balance only changed once,
	// depending on how fast your idempotency cache writes vs reads)
	w1Bal := getBalance(t, db, "wallet_1")
	w2Bal := getBalance(t, db, "wallet_2")

	if w1Bal != 900.00 {
		t.Errorf("Race condition detected! Expected wallet_1 balance to be 900, got %f", w1Bal)
	}
	if w2Bal != 600.00 {
		t.Errorf("Race condition detected! Expected wallet_2 balance to be 600, got %f", w2Bal)
	}
}
