package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
	"wallet-transfer-assignment/pkg/testutil"
)

const testAdminKey = "test-admin-key"

func setupTestServerWithRepos(t *testing.T) (http.Handler, repository.Repositories) {
	t.Helper()
	pool := testutil.SetupTestDB(t)

	repos := repository.Repositories{
		Wallets:     postgres.NewWalletRepository(pool),
		Transfers:   postgres.NewTransferRepository(pool),
		Ledger:      postgres.NewLedgerRepository(pool),
		Idempotency: postgres.NewIdempotencyRepository(pool),
	}
	txManager := postgres.NewTxManager(pool)

	transferService := service.NewTransferService(txManager, repos)
	walletService := service.NewWalletService(txManager, repos)

	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(walletService, testAdminKey)

	return handler.NewRouter(handler.Config{
		TransferHandler: transferHandler,
		WalletHandler:   walletHandler,
	}), repos
}

func setupTestServer(t *testing.T) http.Handler {
	router, _ := setupTestServerWithRepos(t)
	return router
}

func TestHandler_HealthCheck(t *testing.T) {
	router := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_TransferWorkflow(t *testing.T) {
	router := setupTestServer(t)

	// 1. Create Wallet 1
	w1ReqBody, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "hw_1",
		Name:           "Sender Wallet",
		InitialBalance: 1000,
	})
	w1Req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(w1ReqBody))
	w1Req.Header.Set("Content-Type", "application/json")
	w1Req.Header.Set("X-Admin-Key", testAdminKey)
	w1Recorder := httptest.NewRecorder()
	router.ServeHTTP(w1Recorder, w1Req)

	if w1Recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for wallet 1, got %d", w1Recorder.Code)
	}

	// 2. Create Wallet 2
	w2ReqBody, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "hw_2",
		Name:           "Receiver Wallet",
		InitialBalance: 100,
	})
	w2Req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(w2ReqBody))
	w2Req.Header.Set("Content-Type", "application/json")
	w2Req.Header.Set("X-Admin-Key", testAdminKey)
	w2Recorder := httptest.NewRecorder()
	router.ServeHTTP(w2Recorder, w2Req)

	if w2Recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for wallet 2, got %d", w2Recorder.Code)
	}

	// 3. POST /transfers - Successful transfer
	transferReqBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "http_idem_key_1",
		FromWalletID:   "hw_1",
		ToWalletID:     "hw_2",
		Amount:         300,
	})
	txReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(transferReqBody))
	txReq.Header.Set("Content-Type", "application/json")
	txRecorder := httptest.NewRecorder()
	router.ServeHTTP(txRecorder, txReq)

	if txRecorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for transfer, got %d: %s", txRecorder.Code, txRecorder.Body.String())
	}

	var createdTx service.TransferResponse
	if err := json.Unmarshal(txRecorder.Body.Bytes(), &createdTx); err != nil {
		t.Fatalf("failed to decode transfer response: %v", err)
	}
	if createdTx.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected status PROCESSED, got %s", createdTx.Status)
	}

	// 4. POST /transfers - Replay with same idempotency key -> preserves 201 Created
	txReplayReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(transferReqBody))
	txReplayReq.Header.Set("Content-Type", "application/json")
	txReplayRecorder := httptest.NewRecorder()
	router.ServeHTTP(txReplayRecorder, txReplayReq)

	if txReplayRecorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on replay, got %d: %s", txReplayRecorder.Code, txReplayRecorder.Body.String())
	}

	var replayTx service.TransferResponse
	if err := json.Unmarshal(txReplayRecorder.Body.Bytes(), &replayTx); err != nil {
		t.Fatalf("failed to decode replay response: %v", err)
	}
	if !replayTx.IsReplay {
		t.Fatalf("expected isReplay = true on duplicate request")
	}

	// 5. GET /transfers/{id}
	getTxReq := httptest.NewRequest(http.MethodGet, "/transfers/"+createdTx.TransferID, nil)
	getTxRecorder := httptest.NewRecorder()
	router.ServeHTTP(getTxRecorder, getTxReq)

	if getTxRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on get transfer, got %d", getTxRecorder.Code)
	}

	// 6. GET /wallets/{id}
	getW1Req := httptest.NewRequest(http.MethodGet, "/wallets/hw_1", nil)
	getW1Recorder := httptest.NewRecorder()
	router.ServeHTTP(getW1Recorder, getW1Req)

	if getW1Recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on get wallet 1, got %d", getW1Recorder.Code)
	}
	var fetchedW1 domain.Wallet
	_ = json.Unmarshal(getW1Recorder.Body.Bytes(), &fetchedW1)
	if fetchedW1.Balance != 700 {
		t.Fatalf("expected wallet 1 balance 700, got %d", fetchedW1.Balance)
	}

	// 7. GET /wallets/{id}/ledger
	getLedgerReq := httptest.NewRequest(http.MethodGet, "/wallets/hw_1/ledger", nil)
	getLedgerRecorder := httptest.NewRecorder()
	router.ServeHTTP(getLedgerRecorder, getLedgerReq)

	if getLedgerRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on get ledger, got %d", getLedgerRecorder.Code)
	}

	// 8. GET /wallets/{id}/reconcile
	getReconcileReq := httptest.NewRequest(http.MethodGet, "/wallets/hw_1/reconcile", nil)
	getReconcileRecorder := httptest.NewRecorder()
	router.ServeHTTP(getReconcileRecorder, getReconcileReq)

	if getReconcileRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on reconcile, got %d", getReconcileRecorder.Code)
	}

	// 9. Insufficient funds transfer -> 422 Unprocessable Entity
	overdraftBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "overdraft_tx",
		FromWalletID:   "hw_1",
		ToWalletID:     "hw_2",
		Amount:         999999,
	})
	overdraftReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(overdraftBody))
	overdraftReq.Header.Set("Content-Type", "application/json")
	overdraftRecorder := httptest.NewRecorder()
	router.ServeHTTP(overdraftRecorder, overdraftReq)

	if overdraftRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", overdraftRecorder.Code, overdraftRecorder.Body.String())
	}

	// 9b. Replay overdraft transfer -> preserves 422 Unprocessable Entity
	overdraftReplayReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(overdraftBody))
	overdraftReplayReq.Header.Set("Content-Type", "application/json")
	overdraftReplayRecorder := httptest.NewRecorder()
	router.ServeHTTP(overdraftReplayRecorder, overdraftReplayReq)

	if overdraftReplayRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity on overdraft replay, got %d: %s", overdraftReplayRecorder.Code, overdraftReplayRecorder.Body.String())
	}

	// 10. Same wallet transfer -> 400 Bad Request
	selfTransferBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "self_tx",
		FromWalletID:   "hw_1",
		ToWalletID:     "hw_1",
		Amount:         50,
	})
	selfReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(selfTransferBody))
	selfRecorder := httptest.NewRecorder()
	router.ServeHTTP(selfRecorder, selfReq)

	if selfRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on self transfer, got %d", selfRecorder.Code)
	}

	// 11. Empty idempotency key -> 400 Bad Request
	emptyKeyBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "",
		FromWalletID:   "hw_1",
		ToWalletID:     "hw_2",
		Amount:         50,
	})
	emptyKeyReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(emptyKeyBody))
	emptyKeyReq.Header.Set("Content-Type", "application/json")
	emptyKeyRecorder := httptest.NewRecorder()
	router.ServeHTTP(emptyKeyRecorder, emptyKeyReq)

	if emptyKeyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on empty idempotency key, got %d: %s", emptyKeyRecorder.Code, emptyKeyRecorder.Body.String())
	}

	// 12. Empty wallet ID -> 400 Bad Request
	emptyWalletBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "empty_wallet_tx",
		FromWalletID:   "",
		ToWalletID:     "hw_2",
		Amount:         50,
	})
	emptyWalletReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(emptyWalletBody))
	emptyWalletReq.Header.Set("Content-Type", "application/json")
	emptyWalletRecorder := httptest.NewRecorder()
	router.ServeHTTP(emptyWalletRecorder, emptyWalletReq)

	if emptyWalletRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on empty wallet ID, got %d: %s", emptyWalletRecorder.Code, emptyWalletRecorder.Body.String())
	}

	// 13. Non-existent wallet ID -> 404 Not Found
	nonExistentBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "non_existent_wallet_tx",
		FromWalletID:   "does_not_exist_1",
		ToWalletID:     "hw_2",
		Amount:         50,
	})
	nonExistentReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(nonExistentBody))
	nonExistentReq.Header.Set("Content-Type", "application/json")
	nonExistentRecorder := httptest.NewRecorder()
	router.ServeHTTP(nonExistentRecorder, nonExistentReq)

	if nonExistentRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found on non-existent wallet ID, got %d: %s", nonExistentRecorder.Code, nonExistentRecorder.Body.String())
	}
}

func TestHandler_IdempotencyInProgress_Conflict(t *testing.T) {
	router, repos := setupTestServerWithRepos(t)
	ctx := context.Background()

	// Seed source and dest wallets
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_idem_1", Name: "User 1", Balance: 500, Currency: "USD"})
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_idem_2", Name: "User 2", Balance: 500, Currency: "USD"})

	reqBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "concurrent_key_409",
		FromWalletID:   "w_idem_1",
		ToWalletID:     "w_idem_2",
		Amount:         100,
	})

	// Pre-seed an IN_PROGRESS reservation in DB
	reqHash := domain.ComputeRequestHash("w_idem_1", "w_idem_2", 100)
	_, _, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: "concurrent_key_409",
		RequestHash:    reqHash,
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to seed reservation: %v", err)
	}

	// Make HTTP POST request
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Must return HTTP 409 Conflict immediately
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_IdempotencyConflict_PayloadMismatch(t *testing.T) {
	router, repos := setupTestServerWithRepos(t)
	ctx := context.Background()

	// Seed wallets
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_col_1", Name: "User 1", Balance: 500, Currency: "USD"})
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_col_2", Name: "User 2", Balance: 500, Currency: "USD"})
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_col_3", Name: "User 3", Balance: 500, Currency: "USD"})

	// Pre-seed a COMPLETED idempotency record with hash for (w_col_1 -> w_col_2, 100)
	firstHash := domain.ComputeRequestHash("w_col_1", "w_col_2", 100)
	dummyResp := `{"transfer_id":"some_id","status":"PROCESSED"}`
	rec := &domain.IdempotencyRecord{
		IdempotencyKey: "reused_key_mismatch",
		RequestHash:    firstHash,
		Status:         domain.IdempotencyStatusCompleted,
		ResponseCode:   200,
		ResponseBody:   dummyResp,
	}
	_, _, _ = repos.Idempotency.ReserveIdempotency(ctx, rec, 30*time.Second)
	_ = repos.Idempotency.UpdateIdempotency(ctx, rec)

	// Attempt transfer with same key but DIFFERENT recipient (w_col_3 instead of w_col_2)
	reqBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "reused_key_mismatch",
		FromWalletID:   "w_col_1",
		ToWalletID:     "w_col_3",
		Amount:         100,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Must return HTTP 409 Conflict
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for payload mismatch, got %d: %s", w.Code, w.Body.String())
	}

	// Attempt transfer with same key but INVALID amount (-50)
	// Must return HTTP 409 Conflict, NOT 400 Bad Request
	reqInvalidAmount, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "reused_key_mismatch",
		FromWalletID:   "w_col_1",
		ToWalletID:     "w_col_2",
		Amount:         -50,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqInvalidAmount))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for reused key with invalid amount, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandler_Transfer_BalanceOverflow(t *testing.T) {
	router, repos := setupTestServerWithRepos(t)
	ctx := context.Background()

	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_ovf_h1", Name: "Sender", Balance: 500, Currency: "USD"})
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_ovf_h2", Name: "Receiver", Balance: math.MaxInt64 - 50, Currency: "USD"})

	reqBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "overflow_http_key_1",
		FromWalletID:   "w_ovf_h1",
		ToWalletID:     "w_ovf_h2",
		Amount:         100,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on balance overflow, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_SystemTreasury_Reconcile(t *testing.T) {
	router := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/wallets/system_treasury/reconcile", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on system_treasury reconcile, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		WalletID      string `json:"walletId"`
		StoredBalance int64  `json:"storedBalance"`
		LedgerBalance int64  `json:"ledgerBalance"`
		IsBalanced    bool   `json:"isBalanced"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode reconcile response: %v", err)
	}

	const treasuryOpeningBalance int64 = 100000000000000
	if !resp.IsBalanced {
		t.Fatalf("expected system_treasury to be balanced on startup, got %+v", resp)
	}
	if resp.StoredBalance != treasuryOpeningBalance || resp.LedgerBalance != treasuryOpeningBalance {
		t.Fatalf("expected balances %d, got %+v", treasuryOpeningBalance, resp)
	}
}

func TestHandler_Transfer_CurrencyMismatch(t *testing.T) {
	router, repos := setupTestServerWithRepos(t)
	ctx := context.Background()

	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_curr_h1", Name: "USD Wallet", Balance: 500, Currency: "USD"})
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_curr_h2", Name: "EUR Wallet", Balance: 500, Currency: "EUR"})

	reqBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "curr_mismatch_http_1",
		FromWalletID:   "w_curr_h1",
		ToWalletID:     "w_curr_h2",
		Amount:         100,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on currency mismatch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_InternalServerError_GenericMessage(t *testing.T) {
	router, repos := setupTestServerWithRepos(t)
	ctx := context.Background()

	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_ise1", Name: "W1", Balance: 500, Currency: "USD"})
	_ = repos.Wallets.CreateWallet(ctx, &domain.Wallet{ID: "w_ise2", Name: "W2", Balance: 500, Currency: "USD"})

	reqBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "ise_key_1",
		FromWalletID:   "w_ise1",
		ToWalletID:     "w_ise2",
		Amount:         100,
	})

	// Cancel context to force an unexpected failure in repository/database operations
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqBody)).WithContext(canceledCtx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d: %s", w.Code, w.Body.String())
	}

	var errResp handler.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if errResp.Error != "internal server error" {
		t.Fatalf("expected stable generic error 'internal server error', got '%s'", errResp.Error)
	}
}

func TestHandler_GetTransfer_NotFound(t *testing.T) {
	router := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/transfers/non-existent-transfer-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
	}

	var errResp handler.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if errResp.Error != "transfer not found" {
		t.Fatalf("expected 'transfer not found', got '%s'", errResp.Error)
	}
}

func TestHandler_GetTransfer_InternalServerError(t *testing.T) {
	router := setupTestServer(t)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/transfers/some-id", nil).WithContext(canceledCtx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error on operational/db failure, got %d: %s", w.Code, w.Body.String())
	}

	var errResp handler.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if errResp.Error != "internal server error" {
		t.Fatalf("expected stable generic error 'internal server error', got '%s'", errResp.Error)
	}
}

func TestHandler_CreateWallet_Conflict(t *testing.T) {
	router := setupTestServer(t)

	// First creation succeeds
	body1, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "wallet_conflict_test",
		Name:           "Original Wallet",
		InitialBalance: 0,
	})
	req1 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for first wallet, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second creation with identical ID returns 409 Conflict
	body2, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "wallet_conflict_test",
		Name:           "Duplicate Wallet",
		InitialBalance: 0,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate wallet ID, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandler_CreateWallet_InsufficientTreasuryFunds(t *testing.T) {
	router := setupTestServer(t)

	// Treasury balance is 100_000_000_000_000; requesting more with admin authorization must return 422 Unprocessable Entity
	body, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "wallet_excessive_funds",
		Name:           "Billionaire Wallet",
		InitialBalance: 100000000000001,
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", testAdminKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity for initial balance exceeding treasury, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_CreateWallet_UnauthenticatedFundingForbidden(t *testing.T) {
	router := setupTestServer(t)

	// Unauthenticated caller attempts to mint positive balance on public POST /wallets
	body, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "unauth_wallet_funding",
		Name:           "Hacker Wallet",
		InitialBalance: 1000,
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for unauthenticated initial balance funding, got %d: %s", w.Code, w.Body.String())
	}

	var errResp handler.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error != domain.ErrUnauthorizedFunding.Error() {
		t.Fatalf("expected error message %q, got %q", domain.ErrUnauthorizedFunding.Error(), errResp.Error)
	}
}

func TestHandler_CreateWallet_ZeroBalanceAllowedForPublic(t *testing.T) {
	router := setupTestServer(t)

	// Unauthenticated caller creates zero-balance wallet on public POST /wallets
	body, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "public_zero_balance_wallet",
		Name:           "Standard User Wallet",
		InitialBalance: 0,
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for public zero balance wallet creation, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_AdminCreateWallet_Success(t *testing.T) {
	router := setupTestServer(t)

	// Authorized caller calls POST /admin/wallets with X-Admin-Key
	body, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "admin_seeded_wallet",
		Name:           "VIP Seeded Wallet",
		InitialBalance: 500,
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", testAdminKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on POST /admin/wallets with valid key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_AdminCreateWallet_Unauthorized(t *testing.T) {
	router := setupTestServer(t)

	// Caller calls POST /admin/wallets without admin credentials
	body, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "unauthorized_admin_call",
		Name:           "Sneaky Wallet",
		InitialBalance: 500,
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on POST /admin/wallets without admin key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_Transfer_SystemTreasuryForbidden(t *testing.T) {
	router := setupTestServer(t)

	// 1. Attempt transfer FROM system_treasury
	bodyFrom, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "drain_treasury_attempt",
		FromWalletID:   "system_treasury",
		ToWalletID:     "some_user_wallet",
		Amount:         1000,
	})
	reqFrom := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(bodyFrom))
	reqFrom.Header.Set("Content-Type", "application/json")
	wFrom := httptest.NewRecorder()
	router.ServeHTTP(wFrom, reqFrom)

	if wFrom.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for transfer from system_treasury, got %d: %s", wFrom.Code, wFrom.Body.String())
	}

	// 2. Attempt transfer TO system_treasury
	bodyTo, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: "deposit_to_treasury_attempt",
		FromWalletID:   "some_user_wallet",
		ToWalletID:     "system_treasury",
		Amount:         1000,
	})
	reqTo := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(bodyTo))
	reqTo.Header.Set("Content-Type", "application/json")
	wTo := httptest.NewRecorder()
	router.ServeHTTP(wTo, reqTo)

	if wTo.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for transfer to system_treasury, got %d: %s", wTo.Code, wTo.Body.String())
	}
}

func TestHandler_CreateWallet_InternalServerError(t *testing.T) {
	router := setupTestServer(t)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	body, _ := json.Marshal(service.CreateWalletRequest{
		Name:           "Faulty Wallet",
		InitialBalance: 0,
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(body)).WithContext(canceledCtx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d: %s", w.Code, w.Body.String())
	}

	var errResp handler.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if errResp.Error != "internal server error" {
		t.Fatalf("expected stable generic error 'internal server error', got '%s'", errResp.Error)
	}
}

func TestHandler_Transfer_IdempotencyKeyTooLong(t *testing.T) {
	router := setupTestServer(t)

	reqBody, _ := json.Marshal(service.CreateTransferRequest{
		IdempotencyKey: strings.Repeat("k", 129),
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for key > 128 chars, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_Transfer_UnsupportedMediaType(t *testing.T) {
	router := setupTestServer(t)

	reqBody := []byte(`{"idempotencyKey":"key_xml","fromWalletId":"w1","toWalletId":"w2","amount":100}`)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 Unsupported Media Type for text/plain, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_GetWalletLedger_Pagination(t *testing.T) {
	router := setupTestServer(t)

	// Create wallet
	createBody, _ := json.Marshal(service.CreateWalletRequest{
		ID:             "hw_page_1",
		Name:           "Page Test Wallet",
		InitialBalance: 0,
	})
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create wallet: %d", w.Code)
	}

	// 1. Valid pagination query params
	getReq := httptest.NewRequest(http.MethodGet, "/wallets/hw_page_1/ledger?limit=10&offset=0", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid limit/offset, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// 2. Negative limit -> 400 Bad Request
	badLimitReq := httptest.NewRequest(http.MethodGet, "/wallets/hw_page_1/ledger?limit=-1", nil)
	badLimitRec := httptest.NewRecorder()
	router.ServeHTTP(badLimitRec, badLimitReq)
	if badLimitRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for negative limit, got %d", badLimitRec.Code)
	}

	// 3. Non-numeric limit -> 400 Bad Request
	nonNumLimitReq := httptest.NewRequest(http.MethodGet, "/wallets/hw_page_1/ledger?limit=abc", nil)
	nonNumLimitRec := httptest.NewRecorder()
	router.ServeHTTP(nonNumLimitRec, nonNumLimitReq)
	if nonNumLimitRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for non-numeric limit, got %d", nonNumLimitRec.Code)
	}

	// 4. Negative offset -> 400 Bad Request
	badOffsetReq := httptest.NewRequest(http.MethodGet, "/wallets/hw_page_1/ledger?offset=-5", nil)
	badOffsetRec := httptest.NewRecorder()
	router.ServeHTTP(badOffsetRec, badOffsetReq)
	if badOffsetRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for negative offset, got %d", badOffsetRec.Code)
	}
}

func TestWalletHandler_FieldBoundsValidation_Returns400(t *testing.T) {
	router := setupTestServer(t)

	// 1. Overlong Name (> 255 chars)
	longName := strings.Repeat("A", 256)
	overlongNameBody, _ := json.Marshal(map[string]interface{}{
		"id":             "w_valid_id",
		"name":           longName,
		"currency":       "USD",
		"initialBalance": 100,
	})
	req1 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(overlongNameBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-Admin-Key", testAdminKey)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for overlong wallet name, got %d: %s", rec1.Code, rec1.Body.String())
	}
	if !strings.Contains(rec1.Body.String(), "wallet name must not exceed 255 characters") {
		t.Fatalf("expected error message for overlong wallet name, got: %s", rec1.Body.String())
	}

	// 2. Overlong ID (> 64 chars)
	longID := strings.Repeat("x", 65)
	overlongIDBody, _ := json.Marshal(map[string]interface{}{
		"id":             longID,
		"name":           "Valid Name",
		"currency":       "USD",
		"initialBalance": 100,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(overlongIDBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Admin-Key", testAdminKey)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for overlong wallet ID, got %d: %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "wallet ID must not exceed 64 characters") {
		t.Fatalf("expected error message for overlong wallet ID, got: %s", rec2.Body.String())
	}

	// 3. Invalid currency length (!= 3 chars)
	for _, badCurrency := range []string{"US", "USDD", "1", "EUROPE"} {
		badCurrBody, _ := json.Marshal(map[string]interface{}{
			"id":             "w_curr_" + badCurrency,
			"name":           "Valid Name",
			"currency":       badCurrency,
			"initialBalance": 100,
		})
		req3 := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader(badCurrBody))
		req3.Header.Set("Content-Type", "application/json")
		req3.Header.Set("X-Admin-Key", testAdminKey)
		rec3 := httptest.NewRecorder()
		router.ServeHTTP(rec3, req3)
		if rec3.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for invalid currency %q, got %d: %s", badCurrency, rec3.Code, rec3.Body.String())
		}
		if !strings.Contains(rec3.Body.String(), "currency must be a valid 3-letter code") {
			t.Fatalf("expected error message for invalid currency %q, got: %s", badCurrency, rec3.Body.String())
		}
	}

	// 4. Also verify admin wallet creation endpoint (/admin/wallets) enforces bounds and returns 400
	adminLongNameBody, _ := json.Marshal(map[string]interface{}{
		"name":           longName,
		"currency":       "USD",
		"initialBalance": 100,
	})
	adminReq := httptest.NewRequest(http.MethodPost, "/admin/wallets", bytes.NewReader(adminLongNameBody))
	adminReq.Header.Set("Content-Type", "application/json")
	adminReq.Header.Set("X-Admin-Key", testAdminKey)
	adminRec := httptest.NewRecorder()
	router.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for admin wallet overlong name, got %d: %s", adminRec.Code, adminRec.Body.String())
	}
}
