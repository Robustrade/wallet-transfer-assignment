package test_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/handler"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository/memory"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/service"
)

func setupTestServer() http.Handler {
	repo := memory.NewMemoryRepository()
	txService := service.NewTransferService(repo)
	walletService := service.NewWalletService(repo)

	txHandler := handler.NewTransferHandler(txService)
	walletHandler := handler.NewWalletHandler(walletService)
	healthHandler := handler.NewHealthHandler(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler.HealthCheck)
	mux.HandleFunc("POST /transfers", txHandler.CreateTransfer)
	mux.HandleFunc("GET /transfers/", txHandler.GetTransfer)
	mux.HandleFunc("POST /wallets", walletHandler.CreateWallet)
	mux.HandleFunc("GET /wallets/", walletHandler.GetWallet)

	return mux
}

func TestE2E_WalletAndTransferWorkflow(t *testing.T) {
	server := setupTestServer()

	// 1. Create Wallet 1
	w1Req := service.CreateWalletRequest{
		ID:             "user_1",
		InitialBalance: 5000,
		Currency:       "USD",
	}
	body, _ := json.Marshal(w1Req)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("POST", "/wallets", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create wallet 1: %d", rec.Code)
	}

	// 2. Create Wallet 2
	w2Req := service.CreateWalletRequest{
		ID:             "user_2",
		InitialBalance: 2000,
		Currency:       "USD",
	}
	body, _ = json.Marshal(w2Req)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("POST", "/wallets", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create wallet 2: %d", rec.Code)
	}

	// 3. Successful Transfer (1500 from user_1 to user_2)
	txReq := service.TransferRequest{
		IdempotencyKey: "e2e-tx-100",
		FromWalletID:   "user_1",
		ToWalletID:     "user_2",
		Amount:         1500,
	}
	body, _ = json.Marshal(txReq)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("POST", "/transfers", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var txResp service.TransferResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &txResp)
	if txResp.Status != model.TransferStatusProcessed {
		t.Fatalf("expected status PROCESSED, got %s", txResp.Status)
	}

	// 4. Idempotency Replay (Identical request)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("POST", "/transfers", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected replayed 201 Created, got %d", rec.Code)
	}

	// 5. Idempotency Conflict (Same key with different amount)
	conflictReq := service.TransferRequest{
		IdempotencyKey: "e2e-tx-100",
		FromWalletID:   "user_1",
		ToWalletID:     "user_2",
		Amount:         9999,
	}
	body, _ = json.Marshal(conflictReq)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("POST", "/transfers", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on payload mismatch, got %d", rec.Code)
	}

	// 6. Insufficient Funds (user_1 balance is now 3500, try to transfer 4000)
	overdraftReq := service.TransferRequest{
		IdempotencyKey: "e2e-tx-overdraft",
		FromWalletID:   "user_1",
		ToWalletID:     "user_2",
		Amount:         4000,
	}
	body, _ = json.Marshal(overdraftReq)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("POST", "/transfers", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for insufficient funds, got %d", rec.Code)
	}

	// 7. Reconcile Wallets
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("GET", "/wallets/user_1?reconcile=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("failed to reconcile wallet: %d", rec.Code)
	}

	var recon service.WalletReconciliation
	_ = json.Unmarshal(rec.Body.Bytes(), &recon)
	if recon.StoredBalance != 3500 {
		t.Fatalf("expected user_1 balance 3500, got %d", recon.StoredBalance)
	}

	// 8. Health Check
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from health check, got %d", rec.Code)
	}
}
