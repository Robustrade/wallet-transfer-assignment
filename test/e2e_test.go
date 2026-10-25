package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/repository"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/service"
	transporthttp "github.com/dale-tomson/wallet-transfer-assignment/internal/transport/http"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServer struct {
	router     *http.ServeMux
	walletRepo repository.WalletRepository
}

func setupE2EServer(t *testing.T) *testServer {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("test_e2e_%s.db", uuid.NewString()))

	db, err := repository.NewSQLiteDB(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	txMgr := repository.NewSQLTxManager(db)
	walletRepo := repository.NewSQLWalletRepository(db)
	transferRepo := repository.NewSQLTransferRepository(db)
	idempRepo := repository.NewSQLIdempotencyRepository(db)

	svc := service.NewWalletService(txMgr, walletRepo, transferRepo, idempRepo)
	handler := transporthttp.NewWalletHandler(svc)
	router := transporthttp.RegisterRoutes(handler)

	return &testServer{
		router:     router,
		walletRepo: walletRepo,
	}
}

func TestE2E_HealthCheck(t *testing.T) {
	ts := setupE2EServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body["status"])
}

func TestE2E_WalletEndpoints_TableDriven(t *testing.T) {
	ts := setupE2EServer(t)

	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		expectedStatus int
		validate       func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:           "POST /api/v1/wallets: valid creation",
			method:         http.MethodPost,
			path:           "/api/v1/wallets",
			body:           `{"userId":"alice","currency":"USD"}`,
			expectedStatus: http.StatusCreated,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var w domain.Wallet
				err := json.NewDecoder(rec.Body).Decode(&w)
				require.NoError(t, err)
				assert.NotEmpty(t, w.ID)
				assert.Equal(t, "alice", w.UserID)
				assert.Equal(t, "USD", w.Currency)
				assert.Equal(t, int64(0), w.Balance)
			},
		},
		{
			name:           "POST /wallets (root path): valid creation",
			method:         http.MethodPost,
			path:           "/wallets",
			body:           `{"userId":"bob","currency":"EUR"}`,
			expectedStatus: http.StatusCreated,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var w domain.Wallet
				err := json.NewDecoder(rec.Body).Decode(&w)
				require.NoError(t, err)
				assert.NotEmpty(t, w.ID)
				assert.Equal(t, "bob", w.UserID)
				assert.Equal(t, "EUR", w.Currency)
			},
		},
		{
			name:           "POST /api/v1/wallets: missing userId returns 400",
			method:         http.MethodPost,
			path:           "/api/v1/wallets",
			body:           `{"userId":"","currency":"USD"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "POST /api/v1/wallets: missing currency returns 400",
			method:         http.MethodPost,
			path:           "/api/v1/wallets",
			body:           `{"userId":"charlie","currency":""}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "POST /api/v1/wallets: malformed JSON body returns 400",
			method:         http.MethodPost,
			path:           "/api/v1/wallets",
			body:           `{not-json`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "GET /api/v1/wallets/non-existent returns 404",
			method:         http.MethodGet,
			path:           "/api/v1/wallets/non_existent_id",
			body:           "",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			ts.router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			if tt.validate != nil {
				tt.validate(t, rec)
			}
		})
	}
}

func TestE2E_TransferEndpoints_TableDriven(t *testing.T) {
	ts := setupE2EServer(t)
	ctx := context.Background()

	// Seed 2 USD wallets and 1 EUR wallet
	wSenderUSD := &domain.Wallet{
		ID:        "w_sender_usd",
		UserID:    "alice",
		Currency:  "USD",
		Balance:   10000, // 100.00 USD
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	wReceiverUSD := &domain.Wallet{
		ID:        "w_receiver_usd",
		UserID:    "bob",
		Currency:  "USD",
		Balance:   2000, // 20.00 USD
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	wReceiverEUR := &domain.Wallet{
		ID:        "w_receiver_eur",
		UserID:    "clara",
		Currency:  "EUR",
		Balance:   5000,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	require.NoError(t, ts.walletRepo.CreateWallet(ctx, nil, wSenderUSD))
	require.NoError(t, ts.walletRepo.CreateWallet(ctx, nil, wReceiverUSD))
	require.NoError(t, ts.walletRepo.CreateWallet(ctx, nil, wReceiverEUR))

	const happyKey = "happy-idemp-key-1"

	tests := []struct {
		name           string
		path           string
		body           string
		headerKey      string
		expectedStatus int
		validate       func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:           "1. success: valid transfer between USD wallets",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"%s","fromWalletId":"%s","toWalletId":"%s","amount":3000}`, happyKey, wSenderUSD.ID, wReceiverUSD.ID),
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp service.TransferResponse
				err := json.NewDecoder(rec.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotEmpty(t, resp.TransferID)
				assert.Equal(t, "COMPLETED", resp.Status)
				assert.Equal(t, int64(3000), resp.Amount)
				assert.Equal(t, "USD", resp.Currency)

				// Verify database balances updated
				w1, err := ts.walletRepo.GetWalletForUpdate(ctx, nil, wSenderUSD.ID)
				require.NoError(t, err)
				assert.Equal(t, int64(7000), w1.Balance)

				w2, err := ts.walletRepo.GetWalletForUpdate(ctx, nil, wReceiverUSD.ID)
				require.NoError(t, err)
				assert.Equal(t, int64(5000), w2.Balance)
			},
		},
		{
			name:           "2. idempotent replay: returns identical cached response without altering balance",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"%s","fromWalletId":"%s","toWalletId":"%s","amount":3000}`, happyKey, wSenderUSD.ID, wReceiverUSD.ID),
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp service.TransferResponse
				err := json.NewDecoder(rec.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "COMPLETED", resp.Status)
				assert.Equal(t, int64(3000), resp.Amount)

				// Verify balances were NOT debited again
				w1, _ := ts.walletRepo.GetWalletForUpdate(ctx, nil, wSenderUSD.ID)
				assert.Equal(t, int64(7000), w1.Balance)
				w2, _ := ts.walletRepo.GetWalletForUpdate(ctx, nil, wReceiverUSD.ID)
				assert.Equal(t, int64(5000), w2.Balance)
			},
		},
		{
			name:           "3. conflict: same idempotency key with different amount returns 422",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"%s","fromWalletId":"%s","toWalletId":"%s","amount":9999}`, happyKey, wSenderUSD.ID, wReceiverUSD.ID),
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name:           "4. failure: transfer amount exceeding balance returns 422 Unprocessable Entity",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"overdraft-key","fromWalletId":"%s","toWalletId":"%s","amount":50000}`, wSenderUSD.ID, wReceiverUSD.ID),
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name:           "5. failure: currency mismatch returns 400 Bad Request",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"curr-key","fromWalletId":"%s","toWalletId":"%s","amount":500}`, wSenderUSD.ID, wReceiverEUR.ID),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "6. failure: self transfer returns 400 Bad Request",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"self-key","fromWalletId":"%s","toWalletId":"%s","amount":500}`, wSenderUSD.ID, wSenderUSD.ID),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "7. failure: zero or negative amount returns 400 Bad Request",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"zero-key","fromWalletId":"%s","toWalletId":"%s","amount":0}`, wSenderUSD.ID, wReceiverUSD.ID),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "8. failure: missing wallet returns 404 Not Found",
			path:           "/api/v1/transfers",
			body:           fmt.Sprintf(`{"idempotencyKey":"missing-key","fromWalletId":"w_nonexistent","toWalletId":"%s","amount":100}`, wReceiverUSD.ID),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "9. success: header fallback on root /transfers endpoint",
			path:           "/transfers",
			body:           fmt.Sprintf(`{"fromWalletId":"%s","toWalletId":"%s","amount":1000}`, wSenderUSD.ID, wReceiverUSD.ID),
			headerKey:      "header-fallback-key-1",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp service.TransferResponse
				err := json.NewDecoder(rec.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(1000), resp.Amount)

				// Sender balance: 7000 - 1000 = 6000
				w1, _ := ts.walletRepo.GetWalletForUpdate(ctx, nil, wSenderUSD.ID)
				assert.Equal(t, int64(6000), w1.Balance)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(tt.body))
			if tt.headerKey != "" {
				req.Header.Set("Idempotency-Key", tt.headerKey)
			}
			rec := httptest.NewRecorder()
			ts.router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			if tt.validate != nil {
				tt.validate(t, rec)
			}
		})
	}
}
