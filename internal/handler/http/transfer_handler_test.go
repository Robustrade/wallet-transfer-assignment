package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httphandler "wallet-transfer-assignment/internal/handler/http"
	"wallet-transfer-assignment/internal/repository/sqlite"
	"wallet-transfer-assignment/internal/service"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.NewString())
	db, err := sqlite.Open(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	uow := sqlite.NewUnitOfWork(db)
	transferHandler := httphandler.NewTransferHandler(service.NewTransferService(uow))
	walletHandler := httphandler.NewWalletHandler(service.NewWalletService(uow))
	return httphandler.NewRouter(transferHandler, walletHandler)
}

func doJSON(t *testing.T, mux http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCreateTransfer_EndToEnd(t *testing.T) {
	mux := newTestServer(t)

	doJSON(t, mux, http.MethodPost, "/wallets", map[string]any{"id": "wallet_1", "initialBalance": 1000})
	doJSON(t, mux, http.MethodPost, "/wallets", map[string]any{"id": "wallet_2", "initialBalance": 0})

	rec := doJSON(t, mux, http.MethodPost, "/transfers", map[string]any{
		"idempotencyKey": "abc123",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	assert.Equal(t, http.StatusCreated, rec.Code)

	var first map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	assert.Equal(t, "PROCESSED", first["status"])

	// Duplicate request with the same idempotency key must replay, not repeat.
	rec2 := doJSON(t, mux, http.MethodPost, "/transfers", map[string]any{
		"idempotencyKey": "abc123",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	assert.Equal(t, http.StatusOK, rec2.Code)

	var second map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &second))
	assert.Equal(t, first["transferId"], second["transferId"])

	walletRec := httptest.NewRecorder()
	mux.ServeHTTP(walletRec, httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil))
	var wallet map[string]any
	require.NoError(t, json.Unmarshal(walletRec.Body.Bytes(), &wallet))
	assert.Equal(t, float64(900), wallet["balance"])
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	mux := newTestServer(t)
	doJSON(t, mux, http.MethodPost, "/wallets", map[string]any{"id": "wallet_1", "initialBalance": 1000})

	rec := doJSON(t, mux, http.MethodPost, "/transfers", map[string]any{
		"idempotencyKey": "k",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "no_such_wallet",
		"amount":         100,
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateTransfer_InvalidAmount(t *testing.T) {
	mux := newTestServer(t)
	rec := doJSON(t, mux, http.MethodPost, "/transfers", map[string]any{
		"idempotencyKey": "k",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         0,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
