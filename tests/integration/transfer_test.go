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

func TestCreateTransfer_HappyPath(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	reqBody := handler.CreateTransferRequest{
		IdempotencyKey: "test-transfer-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         10000, // $100.00
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.Router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp handler.TransferResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "PROCESSED", resp.Status)
	assert.Equal(t, "wallet_1", resp.FromWalletID)
	assert.Equal(t, "wallet_2", resp.ToWalletID)
	assert.Equal(t, int64(10000), resp.Amount)
	assert.Equal(t, "test-transfer-001", resp.IdempotencyKey)
	assert.Len(t, resp.LedgerEntries, 2)

	// Verify wallet balances
	assert.Equal(t, int64(90000), GetWalletBalance(t, app, "wallet_1"))  // 100000 - 10000
	assert.Equal(t, int64(60000), GetWalletBalance(t, app, "wallet_2"))  // 50000 + 10000

	// Verify ledger entries
	assert.Equal(t, 2, CountLedgerEntries(t, app, resp.ID))
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	reqBody := handler.CreateTransferRequest{
		IdempotencyKey: "test-insufficient-001",
		FromWalletID:   "wallet_3",      // $250.00 balance
		ToWalletID:     "wallet_1",
		Amount:         50000,           // $500.00 — more than wallet_3 has
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.Router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Balances should be unchanged
	assert.Equal(t, int64(25000), GetWalletBalance(t, app, "wallet_3"))
	assert.Equal(t, int64(100000), GetWalletBalance(t, app, "wallet_1"))
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	reqBody := handler.CreateTransferRequest{
		IdempotencyKey: "test-notfound-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_nonexistent",
		Amount:         1000,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.Router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateTransfer_InvalidRequest(t *testing.T) {
	app := SetupTestApp(t)

	tests := []struct {
		name string
		body handler.CreateTransferRequest
	}{
		{
			name: "missing idempotency key",
			body: handler.CreateTransferRequest{
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
		},
		{
			name: "same wallet",
			body: handler.CreateTransferRequest{
				IdempotencyKey: "test-invalid-001",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_1",
				Amount:         100,
			},
		},
		{
			name: "zero amount",
			body: handler.CreateTransferRequest{
				IdempotencyKey: "test-invalid-002",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         0,
			},
		},
		{
			name: "negative amount",
			body: handler.CreateTransferRequest{
				IdempotencyKey: "test-invalid-003",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         -100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			app.Router.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestGetTransfer(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// First create a transfer
	createBody := handler.CreateTransferRequest{
		IdempotencyKey: "test-get-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         5000,
	}

	body, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	app.Router.ServeHTTP(createRec, createReq)

	require.Equal(t, http.StatusCreated, createRec.Code)

	var createResp handler.TransferResponse
	err := json.Unmarshal(createRec.Body.Bytes(), &createResp)
	require.NoError(t, err)

	// Now get the transfer
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/transfers/"+createResp.ID, nil)
	getRec := httptest.NewRecorder()
	app.Router.ServeHTTP(getRec, getReq)

	require.Equal(t, http.StatusOK, getRec.Code)

	var getResp handler.TransferResponse
	err = json.Unmarshal(getRec.Body.Bytes(), &getResp)
	require.NoError(t, err)

	assert.Equal(t, createResp.ID, getResp.ID)
	assert.Equal(t, "PROCESSED", getResp.Status)
	assert.Len(t, getResp.LedgerEntries, 2)
}

func TestGetTransfer_NotFound(t *testing.T) {
	app := SetupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transfers/00000000-0000-0000-0000-000000000000", nil)
	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLedgerConsistency(t *testing.T) {
	app := SetupTestApp(t)
	SeedWallets(t, app)

	// Execute multiple transfers
	transfers := []handler.CreateTransferRequest{
		{IdempotencyKey: "ledger-001", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 10000},
		{IdempotencyKey: "ledger-002", FromWalletID: "wallet_2", ToWalletID: "wallet_3", Amount: 5000},
		{IdempotencyKey: "ledger-003", FromWalletID: "wallet_3", ToWalletID: "wallet_1", Amount: 3000},
	}

	for _, tr := range transfers {
		body, _ := json.Marshal(tr)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.Router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	// Verify ledger always balances: total debits == total credits
	var totalDebits, totalCredits int64
	err := app.Pool.QueryRow(t.Context(),
		`SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'DEBIT'`,
	).Scan(&totalDebits)
	require.NoError(t, err)

	err = app.Pool.QueryRow(t.Context(),
		`SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'CREDIT'`,
	).Scan(&totalCredits)
	require.NoError(t, err)

	assert.Equal(t, totalDebits, totalCredits, "ledger must always balance: debits == credits")

	// Verify wallet balance consistency
	// wallet_1: 100000 - 10000 + 3000 = 93000
	// wallet_2: 50000 + 10000 - 5000 = 55000
	// wallet_3: 25000 + 5000 - 3000 = 27000
	assert.Equal(t, int64(93000), GetWalletBalance(t, app, "wallet_1"))
	assert.Equal(t, int64(55000), GetWalletBalance(t, app, "wallet_2"))
	assert.Equal(t, int64(27000), GetWalletBalance(t, app, "wallet_3"))

	// Verify total balance is conserved (closed system)
	totalBefore := int64(100000 + 50000 + 25000)
	totalAfter := GetWalletBalance(t, app, "wallet_1") + GetWalletBalance(t, app, "wallet_2") + GetWalletBalance(t, app, "wallet_3")
	assert.Equal(t, totalBefore, totalAfter, "total balance must be conserved in a closed system")
}
