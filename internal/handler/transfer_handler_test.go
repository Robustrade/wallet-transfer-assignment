package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"wallet-transfer/internal/config"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/router"
	"wallet-transfer/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupHandlerTestDB(t *testing.T) *gorm.DB {
	cfg := config.Load()
	testDBName := cfg.DBName + "_test"

	err := postgres.EnsureDatabaseExists(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, testDBName, cfg.DBSSLMode)
	require.NoError(t, err)

	testConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, testDBName, cfg.DBSSLMode)

	db, err := postgres.NewDB(testConnStr)
	require.NoError(t, err)

	err = postgres.RunMigrations(db)
	require.NoError(t, err)

	// Clean tables
	err = db.Exec("TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets CASCADE").Error
	require.NoError(t, err)

	return db
}

func TestTransferHandler_Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupHandlerTestDB(t)

	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	transferService := service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	h := handler.NewTransferHandler(transferService, walletRepo, db)

	// Setup Gin Engine via router package
	r := router.Setup(h)

	ctx := context.Background()

	// 1. Create Wallets
	walletA := &domain.Wallet{ID: "w_A", Balance: 1000}
	walletB := &domain.Wallet{ID: "w_B", Balance: 500}
	require.NoError(t, walletRepo.Create(ctx, db, walletA))
	require.NoError(t, walletRepo.Create(ctx, db, walletB))

	// 2. Perform Transfer
	reqPayload := map[string]interface{}{
		"idempotencyKey": "handler_key_1",
		"fromWalletId":   "w_A",
		"toWalletId":     "w_B",
		"amount":         200,
	}
	reqBytes, _ := json.Marshal(reqPayload)

	req, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var respPayload map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &respPayload)
	require.NoError(t, err)
	assert.NotEmpty(t, respPayload["transferId"])
	assert.Equal(t, "PROCESSED", respPayload["status"])

	// 3. Test Idempotency (replay same request)
	req2, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(reqBytes))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusCreated, w2.Code)

	var respPayload2 map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &respPayload2)
	assert.Equal(t, respPayload["transferId"], respPayload2["transferId"])

	// 4. Check Wallets balances via GET /wallets/:id
	reqGet, _ := http.NewRequest(http.MethodGet, "/wallets/w_A", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	assert.Equal(t, http.StatusOK, wGet.Code)

	var walletAResp domain.Wallet
	_ = json.Unmarshal(wGet.Body.Bytes(), &walletAResp)
	assert.Equal(t, int64(800), walletAResp.Balance)
}
