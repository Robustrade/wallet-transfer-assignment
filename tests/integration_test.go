package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/wallet-transfer-assignment/internal/db"
	"github.com/example/wallet-transfer-assignment/internal/domain"
	"github.com/example/wallet-transfer-assignment/internal/handler"
	"github.com/example/wallet-transfer-assignment/internal/repository"
	"github.com/example/wallet-transfer-assignment/internal/service"
)

func setupTestServer(t *testing.T) (*chi.Mux, *sql.DB) {
	database, err := db.Connect("file:" + t.TempDir() + "/test.db?_journal_mode=WAL&_busy_timeout=20000&_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Seed wallets
	walletRepo := repository.NewWalletRepo(database)
	require.NoError(t, walletRepo.Create(context.Background(), "wallet_1", 1000))
	require.NoError(t, walletRepo.Create(context.Background(), "wallet_2", 500))

	transferRepo := repository.NewTransferRepo(database)
	idempotencyRepo := repository.NewIdempotencyRepo(database)
	transferService := service.NewTransferService(database, walletRepo, transferRepo, idempotencyRepo)
	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(transferService)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	handler.SetupRoutes(r, transferHandler, walletHandler)

	return r, database
}

func TestCreateTransfer_Success(t *testing.T) {
	router, _ := setupTestServer(t)

	reqBody := map[string]interface{}{
		"idempotencyKey": uuid.New().String(),
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Logf("Response body: %s", rec.Body.String())
	}
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp service.CreateTransferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, domain.TransferStateProcessed, resp.State)
	assert.NotEmpty(t, resp.TransferID)

	// Verify wallet balances
	req = httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(900), wallet1.Balance)

	req = httptest.NewRequest(http.MethodGet, "/wallets/wallet_2", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var wallet2 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet2))
	assert.Equal(t, int64(600), wallet2.Balance)
}

func TestCreateTransfer_Idempotency(t *testing.T) {
	router, database := setupTestServer(t)

	idempotencyKey := uuid.New().String()
	reqBody := map[string]interface{}{
		"idempotencyKey": idempotencyKey,
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	}
	body, _ := json.Marshal(reqBody)

	// First request
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp1 service.CreateTransferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp1))

	// Duplicate request
	req = httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp2 service.CreateTransferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp2))
	assert.Equal(t, resp1.TransferID, resp2.TransferID)
	assert.Equal(t, "duplicate request", resp2.Message)

	// Verify only one transfer happened by checking balance
	req = httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(900), wallet1.Balance)

	// Verify ledger has exactly 2 entries
	var count int
	err := database.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, resp1.TransferID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	router, _ := setupTestServer(t)

	reqBody := map[string]interface{}{
		"idempotencyKey": uuid.New().String(),
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         2000,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Verify balance unchanged
	req = httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(1000), wallet1.Balance)
}

func TestCreateTransfer_Concurrent(t *testing.T) {
	router, _ := setupTestServer(t)

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0

	// Limit concurrent workers to 3 to keep SQLite happy
	sem := make(chan struct{}, 3)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			reqBody := map[string]interface{}{
				"idempotencyKey": uuid.New().String(),
				"fromWalletId":   "wallet_1",
				"toWalletId":     "wallet_2",
				"amount":         100,
			}
			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code == http.StatusCreated {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, 10, successCount, "all concurrent transfers should succeed")

	// Verify final balance
	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(0), wallet1.Balance)

	req = httptest.NewRequest(http.MethodGet, "/wallets/wallet_2", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var wallet2 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet2))
	assert.Equal(t, int64(1500), wallet2.Balance)
}
