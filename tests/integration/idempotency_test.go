package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdempotency_SameKeyReturnsSameResult(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	reqBody := handler.CreateTransferRequest{
		IdempotencyKey: "idempotent-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         5000,
	}

	body, _ := json.Marshal(reqBody)

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	app.Router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusCreated, rec1.Code)

	var resp1 handler.TransferResponse
	err := json.Unmarshal(rec1.Body.Bytes(), &resp1)
	require.NoError(t, err)

	// Record balance after first request
	balanceAfterFirst1 := GetWalletBalance(t, app, "wallet_1")
	balanceAfterFirst2 := GetWalletBalance(t, app, "wallet_2")

	// Second request with same idempotency key
	body2, _ := json.Marshal(reqBody) // Same request body
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	app.Router.ServeHTTP(rec2, req2)

	// Should return 200 OK (not 201 Created) for duplicate
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp2 handler.TransferResponse
	err = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	require.NoError(t, err)

	// Same transfer ID should be returned
	assert.Equal(t, resp1.ID, resp2.ID, "duplicate request must return same transfer ID")
	assert.Equal(t, resp1.Status, resp2.Status, "duplicate request must return same status")
	assert.Equal(t, resp1.Amount, resp2.Amount, "duplicate request must return same amount")

	// Balances must NOT have changed after the duplicate request
	assert.Equal(t, balanceAfterFirst1, GetWalletBalance(t, app, "wallet_1"),
		"wallet balance must not change on duplicate request")
	assert.Equal(t, balanceAfterFirst2, GetWalletBalance(t, app, "wallet_2"),
		"wallet balance must not change on duplicate request")

	// Ledger entries should still be exactly 2
	assert.Equal(t, 2, CountLedgerEntries(t, app, resp1.ID),
		"no duplicate ledger entries should be created")
}

func TestIdempotency_MultipleRetriesAreAllSafe(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	reqBody := handler.CreateTransferRequest{
		IdempotencyKey: "idempotent-multi-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1000,
	}

	body, _ := json.Marshal(reqBody)
	var firstID string

	// Send the same request 10 times
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.Router.ServeHTTP(rec, req)

		// First should be 201, rest should be 200
		if i == 0 {
			require.Equal(t, http.StatusCreated, rec.Code)
		} else {
			require.Equal(t, http.StatusOK, rec.Code)
		}

		var resp handler.TransferResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)

		if i == 0 {
			firstID = resp.ID
		} else {
			assert.Equal(t, firstID, resp.ID, "all retries must return same transfer ID")
		}
	}

	// Balance should reflect only ONE transfer, not 10
	assert.Equal(t, int64(99000), GetWalletBalance(t, app, "wallet_1")) // 100000 - 1000
	assert.Equal(t, int64(51000), GetWalletBalance(t, app, "wallet_2")) // 50000 + 1000

	// Only 2 ledger entries (1 debit + 1 credit)
	assert.Equal(t, 2, CountLedgerEntries(t, app, firstID))
}

func TestIdempotency_DifferentKeysCreateSeparateTransfers(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// Two transfers with different keys but same details
	for i := 0; i < 2; i++ {
		reqBody := handler.CreateTransferRequest{
			IdempotencyKey: "diff-key-" + string(rune('A'+i)),
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         1000,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.Router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	// Both transfers should have taken effect
	assert.Equal(t, int64(98000), GetWalletBalance(t, app, "wallet_1")) // 100000 - 2*1000
	assert.Equal(t, int64(52000), GetWalletBalance(t, app, "wallet_2")) // 50000 + 2*1000
}

func TestIdempotency_SafeAcrossFailedTransfers(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// First: create a transfer that fails (insufficient funds)
	reqBody := handler.CreateTransferRequest{
		IdempotencyKey: "idempotent-fail-001",
		FromWalletID:   "wallet_3",      // Only $250
		ToWalletID:     "wallet_1",
		Amount:         50000,           // $500 — will fail
	}

	body, _ := json.Marshal(reqBody)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	app.Router.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusUnprocessableEntity, rec1.Code)

	// Retry with same key — should still fail, not process
	body2, _ := json.Marshal(reqBody)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	app.Router.ServeHTTP(rec2, req2)

	// The retry returns the cached FAILED result (200 OK with FAILED status)
	assert.Equal(t, http.StatusOK, rec2.Code)

	var resp handler.TransferResponse
	err := json.Unmarshal(rec2.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "FAILED", resp.Status)

	// Balances unchanged
	assert.Equal(t, int64(25000), GetWalletBalance(t, app, "wallet_3"))
	assert.Equal(t, int64(100000), GetWalletBalance(t, app, "wallet_1"))
}
