package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/shanks/wallet-transfer-assignment/internal/handler"
	"github.com/shanks/wallet-transfer-assignment/internal/repository"
	"github.com/shanks/wallet-transfer-assignment/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHandlerTestRouter(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable"
	}

	db, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, db.PingContext(ctx))

	schemaDDL := `
		CREATE TABLE IF NOT EXISTS wallets (
			id VARCHAR(64) PRIMARY KEY,
			balance BIGINT NOT NULL CHECK (balance >= 0),
			currency VARCHAR(3) NOT NULL DEFAULT 'USD',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS transfers (
			id VARCHAR(64) PRIMARY KEY,
			idempotency_key VARCHAR(255) UNIQUE NOT NULL,
			from_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
			to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
			amount BIGINT NOT NULL CHECK (amount > 0),
			status VARCHAR(32) NOT NULL,
			failure_reason TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS ledger_entries (
			id VARCHAR(64) PRIMARY KEY,
			transfer_id VARCHAR(64) NOT NULL REFERENCES transfers(id),
			wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
			entry_type VARCHAR(16) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
			amount BIGINT NOT NULL CHECK (amount > 0),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT uniq_ledger_transfer_type UNIQUE (transfer_id, entry_type)
		);

		CREATE TABLE IF NOT EXISTS idempotency_records (
			idempotency_key VARCHAR(255) PRIMARY KEY,
			request_hash VARCHAR(64) NOT NULL,
			status VARCHAR(32) NOT NULL,
			response_code INT,
			response_body TEXT,
			transfer_id VARCHAR(64),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets CASCADE;
	`
	_, err = db.ExecContext(ctx, schemaDDL)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})

	txMgr := repository.NewPostgresTxManager(db)
	walletRepo := repository.NewPostgresWalletRepository(db)
	transferRepo := repository.NewPostgresTransferRepository(db)
	ledgerRepo := repository.NewPostgresLedgerRepository(db)
	idempotencyRepo := repository.NewPostgresIdempotencyRepository(db)

	walletSvc := service.NewWalletService(txMgr, walletRepo, transferRepo, ledgerRepo)
	transferSvc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	walletHandler := handler.NewWalletHandler(walletSvc)
	transferHandler := handler.NewTransferHandler(transferSvc)

	router := handler.NewRouter(walletHandler, transferHandler)
	return router, db
}

func TestHealthEndpoint(t *testing.T) {
	router, _ := setupHandlerTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"UP"`)
}

func TestWalletHandler_CreateAndGet(t *testing.T) {
	router, _ := setupHandlerTestRouter(t)

	// Create wallet
	body := []byte(`{"id":"h_w1","initialBalance":1500,"currency":"USD"}`)
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var wallet domain.Wallet
	err := json.Unmarshal(w.Body.Bytes(), &wallet)
	require.NoError(t, err)
	assert.Equal(t, "h_w1", wallet.ID)
	assert.Equal(t, int64(1500), wallet.Balance)

	// Get wallet
	reqGet := httptest.NewRequest(http.MethodGet, "/wallets/h_w1", nil)
	wGet := httptest.NewRecorder()

	router.ServeHTTP(wGet, reqGet)

	assert.Equal(t, http.StatusOK, wGet.Code)
	var getWallet domain.Wallet
	err = json.Unmarshal(wGet.Body.Bytes(), &getWallet)
	require.NoError(t, err)
	assert.Equal(t, "h_w1", getWallet.ID)
}

func TestTransferHandler_CreateTransferAndGet(t *testing.T) {
	router, _ := setupHandlerTestRouter(t)

	// Create 2 wallets first
	b1 := []byte(`{"id":"h_src","initialBalance":2000}`)
	req1 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBuffer(b1))
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	b2 := []byte(`{"id":"h_dst","initialBalance":1000}`)
	req2 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBuffer(b2))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	// POST /transfers
	txPayload := map[string]any{
		"idempotencyKey": "h_idem_key_1",
		"fromWalletId":   "h_src",
		"toWalletId":     "h_dst",
		"amount":         400,
	}
	bodyTx, _ := json.Marshal(txPayload)
	reqTx := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyTx))
	reqTx.Header.Set("Content-Type", "application/json")
	wTx := httptest.NewRecorder()

	router.ServeHTTP(wTx, reqTx)

	assert.Equal(t, http.StatusCreated, wTx.Code)

	var createdTx domain.Transfer
	err := json.Unmarshal(wTx.Body.Bytes(), &createdTx)
	require.NoError(t, err)
	assert.Equal(t, "h_idem_key_1", createdTx.IdempotencyKey)
	assert.Equal(t, domain.TransferStatusProcessed, createdTx.Status)

	// Replay POST /transfers (Idempotency test)
	reqTxReplay := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyTx))
	reqTxReplay.Header.Set("Content-Type", "application/json")
	wTxReplay := httptest.NewRecorder()

	router.ServeHTTP(wTxReplay, reqTxReplay)

	assert.Equal(t, http.StatusOK, wTxReplay.Code)
	var replayedTx domain.Transfer
	_ = json.Unmarshal(wTxReplay.Body.Bytes(), &replayedTx)
	assert.Equal(t, createdTx.ID, replayedTx.ID)

	// GET /transfers/{id}
	reqGetTx := httptest.NewRequest(http.MethodGet, "/transfers/"+createdTx.ID, nil)
	wGetTx := httptest.NewRecorder()
	router.ServeHTTP(wGetTx, reqGetTx)

	assert.Equal(t, http.StatusOK, wGetTx.Code)
	var getResp struct {
		Transfer      domain.Transfer      `json:"transfer"`
		LedgerEntries []domain.LedgerEntry `json:"ledgerEntries"`
	}
	err = json.Unmarshal(wGetTx.Body.Bytes(), &getResp)
	require.NoError(t, err)
	assert.Equal(t, createdTx.ID, getResp.Transfer.ID)
	assert.Len(t, getResp.LedgerEntries, 2)
}
