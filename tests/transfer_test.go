package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"wallet-transfer-service/internal/config"
	"wallet-transfer-service/internal/handler"
	"wallet-transfer-service/internal/model"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/routes"
	"wallet-transfer-service/internal/service"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *sql.DB) {
	gin.SetMode(gin.TestMode)

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/walletDB_test?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx), "Postgres must be reachable for integration testing")

	// Clean tables before each run
	_, err = db.ExecContext(ctx, "TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets CASCADE")
	require.NoError(t, err)

	cfg := &config.Config{
		IdempotencyLockTTL:   10 * time.Second,
		IdempotencyRetention: 24 * time.Hour,
	}

	walletRepo := repository.NewWalletRepository(db)
	transferRepo := repository.NewTransferRepository(db)
	idempotencyRepo := repository.NewIdempotencyRepository(db)

	walletService := service.NewWalletService(walletRepo, transferRepo)
	transferService := service.NewTransferService(transferRepo, idempotencyRepo, cfg)

	walletHandler := handler.NewWalletHandler(walletService)
	transferHandler := handler.NewTransferHandler(transferService)

	router := routes.SetupRouter(walletHandler, transferHandler, []string{"http://localhost:3000"})

	return router, db
}

func createWalletForTest(t *testing.T, router *gin.Engine, id, initialBalance string) {
	reqBody := fmt.Sprintf(`{"id":"%s","initialBalance":"%s","status":"ACTIVE"}`, id, initialBalance)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "wallet creation failed: %s", w.Body.String())
}

func TestTransfer_SuccessfulExecutionAndDoubleEntryLedger(t *testing.T) {
	router, db := setupTestRouter(t)
	defer db.Close()

	createWalletForTest(t, router, "W_ALICE", "100.00")
	createWalletForTest(t, router, "W_BOB", "50.00")

	transferReq := model.CreateTransferRequest{
		IdempotencyKey: "tx_test_success_001",
		FromWalletID:   "W_ALICE",
		ToWalletID:     "W_BOB",
		Amount:         "30.00",
	}
	body, _ := json.Marshal(transferReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code, "transfer failed: %s", w.Body.String())

	var resp model.CreateTransferResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "PROCESSED", string(resp.Status))
	assert.Equal(t, "30.00", resp.Amount)

	// 1. Verify ledger produced exactly 2 balancing records
	var ledgerCount int
	err := db.QueryRow("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1", resp.TransferID).Scan(&ledgerCount)
	require.NoError(t, err)
	assert.Equal(t, 2, ledgerCount)

	// 2. Verify audit snapshots in ledger_entries
	rows, err := db.Query(`
		SELECT wallet_id, type, amount, balance_before, balance_after 
		FROM ledger_entries 
		WHERE transfer_id = $1 
		ORDER BY type ASC
	`, resp.TransferID)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var wID, lType string
		var amt, before, after int64
		require.NoError(t, rows.Scan(&wID, &lType, &amt, &before, &after))

		if lType == "CREDIT" {
			assert.Equal(t, "W_BOB", wID)
			assert.Equal(t, int64(3000), amt)
			assert.Equal(t, int64(5000), before)
			assert.Equal(t, int64(8000), after)
		} else {
			assert.Equal(t, "W_ALICE", wID)
			assert.Equal(t, int64(3000), amt)
			assert.Equal(t, int64(10000), before)
			assert.Equal(t, int64(7000), after)
		}
	}
	require.NoError(t, rows.Err())
}

func TestTransfer_IdempotencyReplay(t *testing.T) {
	router, db := setupTestRouter(t)
	defer db.Close()

	createWalletForTest(t, router, "W_ALICE", "100.00")
	createWalletForTest(t, router, "W_BOB", "50.00")

	transferReq := model.CreateTransferRequest{
		IdempotencyKey: "tx_idempotent_key_123",
		FromWalletID:   "W_ALICE",
		ToWalletID:     "W_BOB",
		Amount:         "40.00",
	}
	body, _ := json.Marshal(transferReq)

	// First Request
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBuffer(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusCreated, w1.Code)

	// Second Request with identical idempotency key and payload
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBuffer(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusCreated, w2.Code)

	// Responses must be completely identical
	assert.JSONEq(t, w1.Body.String(), w2.Body.String())

	// Ensure no second transfer was executed and Alice was only debited once
	var aliceBalance int64
	err := db.QueryRow("SELECT balance FROM wallets WHERE id = 'W_ALICE'").Scan(&aliceBalance)
	require.NoError(t, err)
	assert.Equal(t, int64(6000), aliceBalance)
}

func TestTransfer_IdempotencyPayloadMismatch(t *testing.T) {
	router, db := setupTestRouter(t)
	defer db.Close()

	createWalletForTest(t, router, "W_ALICE", "100.00")
	createWalletForTest(t, router, "W_BOB", "50.00")

	// Call 1: Original transfer with amount 10.00
	req1Data := model.CreateTransferRequest{
		IdempotencyKey: "reused_key_different_payload",
		FromWalletID:   "W_ALICE",
		ToWalletID:     "W_BOB",
		Amount:         "10.00",
	}
	body1, _ := json.Marshal(req1Data)
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBuffer(body1))
	req1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusCreated, w1.Code)

	// Call 2: Tampered request with altered amount 25.00
	req2Data := model.CreateTransferRequest{
		IdempotencyKey: "reused_key_different_payload",
		FromWalletID:   "W_ALICE",
		ToWalletID:     "W_BOB",
		Amount:         "25.00",
	}
	body2, _ := json.Marshal(req2Data)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusBadRequest, w2.Code)
	assert.Contains(t, w2.Body.String(), "idempotency key already used with different payload")
}

func TestTransfer_InsufficientBalance(t *testing.T) {
	router, db := setupTestRouter(t)
	defer db.Close()

	createWalletForTest(t, router, "W_POOR", "5.00")
	createWalletForTest(t, router, "W_BOB", "50.00")

	reqData := model.CreateTransferRequest{
		IdempotencyKey: "tx_insufficient_001",
		FromWalletID:   "W_POOR",
		ToWalletID:     "W_BOB",
		Amount:         "50.00",
	}
	body, _ := json.Marshal(reqData)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json") // Fixes 415 error
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "insufficient balance")
}

func TestTransfer_HighConcurrencyNoDoubleSpend(t *testing.T) {
	router, db := setupTestRouter(t)
	defer db.Close()

	// Initial balance: 100.00. 10 simultaneous transfers of 15.00 each = 150.00 total.
	// Exactly 6 transfers can succeed (6 * 15 = 90.00, leaving 10.00). 4 must fail.
	createWalletForTest(t, router, "W_CONCURRENT_SRC", "100.00")
	createWalletForTest(t, router, "W_CONCURRENT_DST", "0.00")

	concurrentRequests := 10
	var wg sync.WaitGroup
	results := make([]int, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := model.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent_key_%d", idx),
				FromWalletID:   "W_CONCURRENT_SRC",
				ToWalletID:     "W_CONCURRENT_DST",
				Amount:         "15.00",
			}
			b, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBuffer(b))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results[idx] = w.Code
		}(i)
	}

	wg.Wait()

	successCount := 0
	badRequestCount := 0
	for _, code := range results {
		if code == http.StatusCreated {
			successCount++
		} else if code == http.StatusBadRequest {
			badRequestCount++
		}
	}

	assert.Equal(t, 6, successCount, "Exactly 6 requests must succeed before balance depletes")
	assert.Equal(t, 4, badRequestCount, "Exactly 4 requests must fail with insufficient balance")

	// Assert final database state
	var srcFinal, dstFinal int64
	err := db.QueryRow("SELECT balance FROM wallets WHERE id = 'W_CONCURRENT_SRC'").Scan(&srcFinal)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), srcFinal) // 10000 - (6 * 1500) = 1000 ($10.00)

	err = db.QueryRow("SELECT balance FROM wallets WHERE id = 'W_CONCURRENT_DST'").Scan(&dstFinal)
	require.NoError(t, err)
	assert.Equal(t, int64(9000), dstFinal) // 0 + (6 * 1500) = 9000 ($90.00)
}
