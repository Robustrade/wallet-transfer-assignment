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

func TestWalletLifecycle(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	client := ts.Client()
	walletID := fmt.Sprintf("wallet_%d", time.Now().UnixNano())

	// 1. Create Wallet successfully
	createBody, err := json.Marshal(map[string]any{
		"id":      walletID,
		"balance": 10000,
	})
	require.NoError(t, err)

	resp, err := client.Post(ts.URL+"/wallets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created domain.Wallet
	err = json.NewDecoder(resp.Body).Decode(&created)
	require.NoError(t, err)
	assert.Equal(t, walletID, created.ID)
	assert.Equal(t, int64(10000), created.Balance)

	// 2. Get Wallet successfully
	getResp, err := client.Get(ts.URL + "/wallets/" + walletID)
	require.NoError(t, err)
	defer func() { _ = getResp.Body.Close() }()
	assert.Equal(t, http.StatusOK, getResp.StatusCode)

	var fetched domain.Wallet
	err = json.NewDecoder(getResp.Body).Decode(&fetched)
	require.NoError(t, err)
	assert.Equal(t, walletID, fetched.ID)
	assert.Equal(t, int64(10000), fetched.Balance)

	// 3. Get non-existent Wallet returns 404
	missingResp, err := client.Get(ts.URL + "/wallets/unknown_wallet_id")
	require.NoError(t, err)
	defer func() { _ = missingResp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, missingResp.StatusCode)

	// 4. Create Wallet with negative balance returns 400
	negBody, err := json.Marshal(map[string]any{
		"id":      "neg_wallet",
		"balance": -500,
	})
	require.NoError(t, err)

	negResp, err := client.Post(ts.URL+"/wallets", "application/json", bytes.NewReader(negBody))
	require.NoError(t, err)
	defer func() { _ = negResp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, negResp.StatusCode)
}
