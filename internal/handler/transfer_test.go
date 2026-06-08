package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aks-95/wallet-transfer-assignment/internal/handler"
	"github.com/aks-95/wallet-transfer-assignment/internal/migrate"
	"github.com/aks-95/wallet-transfer-assignment/internal/repository"
	"github.com/aks-95/wallet-transfer-assignment/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRouter(t *testing.T) (http.Handler, *repository.DB) {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:root@localhost:5432/wallet_transfer?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	db, err := repository.New(ctx, url)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}

	ctx = context.Background()
	require.NoError(t, migrate.Up(ctx, db.Pool))
	_, err = db.Pool.Exec(ctx, `TRUNCATE TABLE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	t.Cleanup(db.Close)

	router := handler.NewRouter(handler.Dependencies{
		TransferService: service.NewTransferService(db),
	})
	return router, db
}

func postTransfer(t *testing.T, router http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCreateTransferHTTPCreated(t *testing.T) {
	router, db := testRouter(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	rec := postTransfer(t, router, map[string]any{
		"idempotencyKey": "http-created",
		"fromWalletId":   from.ID.String(),
		"toWalletId":     to.ID.String(),
		"amount":         150,
	})

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "PROCESSED", resp["status"])
	assert.Equal(t, float64(150), resp["amount"])
}

func TestCreateTransferHTTPReplay(t *testing.T) {
	router, db := testRouter(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	body := map[string]any{
		"idempotencyKey": "http-replay",
		"fromWalletId":   from.ID.String(),
		"toWalletId":     to.ID.String(),
		"amount":         100,
	}

	first := postTransfer(t, router, body)
	require.Equal(t, http.StatusCreated, first.Code)

	second := postTransfer(t, router, body)
	assert.Equal(t, http.StatusOK, second.Code)

	var firstResp map[string]any
	var secondResp map[string]any
	require.NoError(t, json.NewDecoder(first.Body).Decode(&firstResp))
	require.NoError(t, json.NewDecoder(second.Body).Decode(&secondResp))
	assert.Equal(t, firstResp["id"], secondResp["id"])
}

func TestCreateTransferHTTPBadRequest(t *testing.T) {
	router, _ := testRouter(t)

	rec := postTransfer(t, router, map[string]any{
		"idempotencyKey": "",
		"fromWalletId":   "wallet-1",
		"toWalletId":     "wallet-2",
		"amount":         100,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "idempotencyKey is required", resp["error"])
}

func TestCreateTransferHTTPInsufficientFunds(t *testing.T) {
	router, db := testRouter(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 50)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	rec := postTransfer(t, router, map[string]any{
		"idempotencyKey": "http-insufficient",
		"fromWalletId":   from.ID.String(),
		"toWalletId":     to.ID.String(),
		"amount":         100,
	})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "FAILED", resp["status"])
}

func TestCreateTransferHTTPWalletNotFound(t *testing.T) {
	router, db := testRouter(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)

	rec := postTransfer(t, router, map[string]any{
		"idempotencyKey": "http-missing-wallet",
		"fromWalletId":   from.ID.String(),
		"toWalletId":     "00000000-0000-0000-0000-000000000099",
		"amount":         100,
	})

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "wallet not found", resp["error"])
}

func TestCreateTransferHTTPIdempotencyConflict(t *testing.T) {
	router, db := testRouter(t)
	ctx := context.Background()

	from, err := db.CreateWallet(ctx, 1000)
	require.NoError(t, err)
	to, err := db.CreateWallet(ctx, 0)
	require.NoError(t, err)

	postTransfer(t, router, map[string]any{
		"idempotencyKey": "http-conflict",
		"fromWalletId":   from.ID.String(),
		"toWalletId":     to.ID.String(),
		"amount":         100,
	})

	rec := postTransfer(t, router, map[string]any{
		"idempotencyKey": "http-conflict",
		"fromWalletId":   from.ID.String(),
		"toWalletId":     to.ID.String(),
		"amount":         200,
	})

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestCreateTransferHTTPInvalidJSON(t *testing.T) {
	router, _ := testRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
