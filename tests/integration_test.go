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

func postTransfer(router *chi.Mux, key, from, to string, amount int64) (*httptest.ResponseRecorder, error) {
	reqBody := map[string]interface{}{
		"idempotencyKey": key,
		"fromWalletId":   from,
		"toWalletId":     to,
		"amount":         amount,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, nil
}

func getWallet(router *chi.Mux, id string) (*httptest.ResponseRecorder, error) {
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+id, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, nil
}

func TestCreateTransfer_Success(t *testing.T) {
	router, _ := setupTestServer(t)

	rec, err := postTransfer(router, uuid.New().String(), "wallet_1", "wallet_2", 100)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp service.CreateTransferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, domain.TransferStateProcessed, resp.State)
	assert.NotEmpty(t, resp.TransferID)

	rec, err = getWallet(router, "wallet_1")
	require.NoError(t, err)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(900), wallet1.Balance)

	rec, err = getWallet(router, "wallet_2")
	require.NoError(t, err)
	var wallet2 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet2))
	assert.Equal(t, int64(600), wallet2.Balance)
}

func TestCreateTransfer_Idempotency(t *testing.T) {
	router, database := setupTestServer(t)

	key := uuid.New().String()

	rec, err := postTransfer(router, key, "wallet_1", "wallet_2", 100)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp1 service.CreateTransferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp1))

	rec, err = postTransfer(router, key, "wallet_1", "wallet_2", 100)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp2 service.CreateTransferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp2))
	assert.Equal(t, resp1.TransferID, resp2.TransferID)
	assert.Equal(t, "duplicate request", resp2.Message)

	rec, err = getWallet(router, "wallet_1")
	require.NoError(t, err)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(900), wallet1.Balance)

	var count int
	err = database.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, resp1.TransferID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	router, _ := setupTestServer(t)

	rec, err := postTransfer(router, uuid.New().String(), "wallet_1", "wallet_2", 2000)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	rec, err = getWallet(router, "wallet_1")
	require.NoError(t, err)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(1000), wallet1.Balance)
}

func TestCreateTransfer_Concurrent_DifferentKeys(t *testing.T) {
	router, _ := setupTestServer(t)

	// wallet_1 has 1000, each transfer is 100
	// 10 concurrent goroutines, different keys, all debiting wallet_1
	const count = 10
	var wg sync.WaitGroup
	errs := make(chan error, count)
	statuses := make(chan int, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			rec, err := postTransfer(router, uuid.New().String(), "wallet_1", "wallet_2", 100)
			if err != nil {
				errs <- err
			} else {
				statuses <- rec.Code
			}
		}()
		_ = i
	}

	wg.Wait()
	close(errs)
	close(statuses)

	for err := range errs {
		require.NoError(t, err)
	}

	var created, ok int
	for code := range statuses {
		if code == http.StatusCreated {
			created++
		} else if code == http.StatusOK {
			ok++
		}
	}

	assert.Equal(t, count, created, "all %d concurrent transfers should succeed (created)", count)
	assert.Equal(t, 0, ok, "no duplicates expected")

	rec, err := getWallet(router, "wallet_1")
	require.NoError(t, err)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(0), wallet1.Balance, "wallet_1 should be empty after 10x100 transfers")

	rec, err = getWallet(router, "wallet_2")
	require.NoError(t, err)
	var wallet2 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet2))
	assert.Equal(t, int64(1500), wallet2.Balance, "wallet_2 should have original 500 + 1000")
}

func TestCreateTransfer_Idempotency_Race(t *testing.T) {
	router, database := setupTestServer(t)

	// 10 concurrent requests with the SAME idempotency key
	// Only 1 should create a transfer, the rest should get duplicate response
	key := uuid.New().String()
	const count = 10
	var wg sync.WaitGroup
	statuses := make(chan int, count)
	ids := make(chan string, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := postTransfer(router, key, "wallet_1", "wallet_2", 100)
			if err != nil {
				return
			}
			statuses <- rec.Code
			var resp service.CreateTransferResponse
			if json.Unmarshal(rec.Body.Bytes(), &resp) == nil {
				ids <- resp.TransferID
			}
		}()
	}

	wg.Wait()
	close(statuses)
	close(ids)

	var created int
	var duplicate int
	for code := range statuses {
		if code == http.StatusCreated {
			created++
		} else if code == http.StatusOK {
			duplicate++
		}
	}

	assert.Equal(t, 1, created, "exactly 1 transfer should be created")
	assert.Equal(t, count-1, duplicate, "remaining %d should be duplicates", count-1)

	// All goroutines should report the same transfer ID
	var firstID string
	for id := range ids {
		if firstID == "" {
			firstID = id
		} else {
			assert.Equal(t, firstID, id, "all responses should reference the same transfer")
		}
	}
	assert.NotEmpty(t, firstID)

	// Verify exactly 2 ledger entries exist for this transfer
	var ledgerCount int
	err := database.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, firstID).Scan(&ledgerCount)
	require.NoError(t, err)
	assert.Equal(t, 2, ledgerCount, "exactly 2 ledger entries for the transfer")

	// Verify balance only changed once
	rec, err := getWallet(router, "wallet_1")
	require.NoError(t, err)
	var wallet1 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet1))
	assert.Equal(t, int64(900), wallet1.Balance, "balance should only be debited once")

	rec, err = getWallet(router, "wallet_2")
	require.NoError(t, err)
	var wallet2 domain.Wallet
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wallet2))
	assert.Equal(t, int64(600), wallet2.Balance, "balance should only be credited once")
}
