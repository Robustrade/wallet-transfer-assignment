package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/migrations"
	"wallet-transfer-assignment/internal/service"
)

func setupTestServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/wallet_db?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)

	err = migrations.Run(ctx, pool)
	require.NoError(t, err)

	txManager := repository.NewTransactor(pool)
	walletRepo := repository.NewWalletRepository(pool)
	walletSvc := service.NewWalletService(walletRepo)
	walletHandler := handler.NewWalletHandler(walletSvc)

	transferRepo := repository.NewTransferRepository(pool)
	ledgerRepo := repository.NewLedgerRepository(pool)
	idempRepo := repository.NewIdempotencyRepository(pool)
	transferSvc := service.NewTransferService(txManager, walletRepo, transferRepo, ledgerRepo, idempRepo)
	transferHandler := handler.NewTransferHandler(transferSvc)

	router := handler.NewRouter(walletHandler, transferHandler)

	ts := httptest.NewServer(router)
	return ts, pool
}

func TestWallet_Create_Success(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	walletID := fmt.Sprintf("wallet_%d", time.Now().UnixNano())
	var w domain.Wallet = createTestWallet(t, ts, walletID, 10000)
	assert.Equal(t, walletID, w.ID)
	assert.Equal(t, int64(10000), w.Balance)
	assert.False(t, w.CreatedAt.IsZero())
	assert.False(t, w.UpdatedAt.IsZero())
}

func TestWallet_Create_DuplicateID(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	walletID := fmt.Sprintf("wallet_dup_%d", time.Now().UnixNano())
	createTestWallet(t, ts, walletID, 5000)

	body, err := json.Marshal(map[string]any{
		"id":      walletID,
		"balance": 2000,
	})
	require.NoError(t, err)

	resp, err := ts.Client().Post(ts.URL+"/wallets", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var errBody map[string]any
	err = json.NewDecoder(resp.Body).Decode(&errBody)
	require.NoError(t, err)
	assert.Equal(t, "CONFLICT", errBody["error"])
	assert.Equal(t, "wallet with this id already exists", errBody["message"])
}

func TestWallet_Create_NegativeBalance(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	body, err := json.Marshal(map[string]any{
		"id":      "neg_wallet",
		"balance": -500,
	})
	require.NoError(t, err)

	resp, err := ts.Client().Post(ts.URL+"/wallets", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var errBody map[string]any
	err = json.NewDecoder(resp.Body).Decode(&errBody)
	require.NoError(t, err)
	assert.Equal(t, "VALIDATION_ERROR", errBody["error"])
	assert.Equal(t, "wallet balance cannot be negative", errBody["message"])
}

func TestWallet_Create_EmptyID(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	body, err := json.Marshal(map[string]any{
		"id":      "   ",
		"balance": 500,
	})
	require.NoError(t, err)

	resp, err := ts.Client().Post(ts.URL+"/wallets", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var errBody map[string]any
	err = json.NewDecoder(resp.Body).Decode(&errBody)
	require.NoError(t, err)
	assert.Equal(t, "VALIDATION_ERROR", errBody["error"])
	assert.Equal(t, "wallet id cannot be empty", errBody["message"])
}

func TestWallet_Create_TrailingJSON(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	payload := []byte(`{"id":"wallet_trailing","balance":100} {"extra":"data"}`)
	resp, err := ts.Client().Post(ts.URL+"/wallets", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var errBody map[string]any
	err = json.NewDecoder(resp.Body).Decode(&errBody)
	require.NoError(t, err)
	assert.Equal(t, "INVALID_REQUEST", errBody["error"])
	assert.Equal(t, "request body must only contain a single JSON object", errBody["message"])
}

func TestWallet_Get_Success(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	walletID := fmt.Sprintf("wallet_get_%d", time.Now().UnixNano())
	created := createTestWallet(t, ts, walletID, 7500)

	fetched := getTestWallet(t, ts, walletID)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, created.Balance, fetched.Balance)
}

func TestWallet_Get_NotFound(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	resp, err := ts.Client().Get(ts.URL + "/wallets/non_existent_wallet_id")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	var errBody map[string]any
	err = json.NewDecoder(resp.Body).Decode(&errBody)
	require.NoError(t, err)
	assert.Equal(t, "NOT_FOUND", errBody["error"])
	assert.Equal(t, "wallet not found", errBody["message"])
}
