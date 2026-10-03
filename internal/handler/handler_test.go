package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testEnv struct {
	echo            *echo.Echo
	walletHandler   *handler.WalletHandler
	transferHandler *handler.TransferHandler
	healthHandler   *handler.HealthHandler
	walletService   service.WalletService
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "handler_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = database.AutoMigrate(db)
	require.NoError(t, err)

	repo := postgres.NewRepository(db)
	transferSvc := service.NewTransferService(repo)
	walletSvc := service.NewWalletService(repo)

	walletHandler := handler.NewWalletHandler(walletSvc)
	transferHandler := handler.NewTransferHandler(transferSvc)
	healthHandler := handler.NewHealthHandler("wallet-transfer-test")

	e := echo.New()

	return &testEnv{
		echo:            e,
		walletHandler:   walletHandler,
		transferHandler: transferHandler,
		healthHandler:   healthHandler,
		walletService:   walletSvc,
	}
}

func TestHealthHandler(t *testing.T) {
	env := setupTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := env.echo.NewContext(req, rec)

	err := env.healthHandler.Check(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "wallet-transfer-test")
}

func TestWalletHandler_CreateAndGetBalance(t *testing.T) {
	env := setupTestEnv(t)

	// 1. Create Wallet
	body, _ := json.Marshal(map[string]interface{}{
		"id":             "w_user1",
		"initialBalance": 500,
		"currency":       "USD",
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := env.echo.NewContext(req, rec)

	err := env.walletHandler.CreateWallet(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// 2. Get Balance
	req = httptest.NewRequest(http.MethodGet, "/wallets/w_user1/balance", nil)
	rec = httptest.NewRecorder()
	c = env.echo.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("w_user1")

	err = env.walletHandler.GetBalance(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"storedBalance":500`)
	assert.Contains(t, rec.Body.String(), `"isAuditConsistent":true`)
}

func TestTransferHandler_FullFlow(t *testing.T) {
	env := setupTestEnv(t)
	ctx := t.Context()

	// Seed wallets
	_, err := env.walletService.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_sender",
		InitialBalance: 1000,
	})
	require.NoError(t, err)

	_, err = env.walletService.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_receiver",
		InitialBalance: 100,
	})
	require.NoError(t, err)

	// 1. Successful Transfer (201 Created)
	transferReq := service.CreateTransferRequest{
		IdempotencyKey: "test-idem-key-01",
		FromWalletID:   "w_sender",
		ToWalletID:     "w_receiver",
		Amount:         250,
	}
	body, _ := json.Marshal(transferReq)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := env.echo.NewContext(req, rec)

	err = env.transferHandler.CreateTransfer(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var createdResp service.TransferResponse
	err = json.Unmarshal(rec.Body.Bytes(), &createdResp)
	require.NoError(t, err)
	assert.Equal(t, domain.TransferStatusProcessed, createdResp.Status)
	assert.Equal(t, int64(250), createdResp.Amount)

	// 2. Idempotent Replay (200 OK)
	req = httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = env.echo.NewContext(req, rec)

	err = env.transferHandler.CreateTransfer(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var replayedResp service.TransferResponse
	err = json.Unmarshal(rec.Body.Bytes(), &replayedResp)
	require.NoError(t, err)
	assert.Equal(t, createdResp.TransferID, replayedResp.TransferID)

	// 3. Inspect Ledger (GET /transfers/:id/ledger)
	req = httptest.NewRequest(http.MethodGet, "/transfers/"+createdResp.TransferID+"/ledger", nil)
	rec = httptest.NewRecorder()
	c = env.echo.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(createdResp.TransferID)

	err = env.transferHandler.GetTransferLedger(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"DEBIT"`)
	assert.Contains(t, rec.Body.String(), `"CREDIT"`)

	// 4. Insufficient Balance Transfer (422 Unprocessable Entity)
	overdraftReq := service.CreateTransferRequest{
		IdempotencyKey: "test-overdraft-key",
		FromWalletID:   "w_sender",
		ToWalletID:     "w_receiver",
		Amount:         50000,
	}
	body, _ = json.Marshal(overdraftReq)
	req = httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = env.echo.NewContext(req, rec)

	err = env.transferHandler.CreateTransfer(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "FAILED")

	// 5. Currency Mismatch Transfer (422 Unprocessable Entity)
	currMismatchReq := service.CreateTransferRequest{
		IdempotencyKey: "test-curr-mismatch-key",
		FromWalletID:   "w_sender",
		ToWalletID:     "w_receiver",
		Amount:         10,
		Currency:       "EUR", // w_sender and w_receiver are USD
	}
	body, _ = json.Marshal(currMismatchReq)
	req = httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = env.echo.NewContext(req, rec)

	err = env.transferHandler.CreateTransfer(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "FAILED")
}
