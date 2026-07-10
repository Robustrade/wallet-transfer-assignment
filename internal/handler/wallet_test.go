package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/dto"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/handler"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/testutils"
)

func TestTransferHandler(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("TransferFunds", mock.Anything, "test-idempotency-key", "wallet1", "wallet2", 100.0).Return("txn_123", "success", int64(1234567890), nil)

	body, _ := json.Marshal(dto.TransferRequest{
		IdempotencyKey: "test-idempotency-key",
		FromWalletID:   "wallet1",
		ToWalletID:     "wallet2",
		Amount:         100.0,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.TransferHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp dto.TransferResponse
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "txn_123", resp.TransactionID)
	assert.Equal(t, "success", resp.Status)

	mockWalletSvc.AssertExpectations(t)
}

func TestTransferHandler_InvalidRequest_WalletIDCannotBeSame(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	body, _ := json.Marshal(dto.TransferRequest{
		IdempotencyKey: "test-idempotency-key",
		FromWalletID:   "wallet1",
		ToWalletID:     "wallet1", // Same wallet ID to trigger validation error
		Amount:         100.0,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.TransferHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "from and to wallet cannot be the same")
}

func TestTransferHandler_InvalidRequest_NegativeAmount(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	body, _ := json.Marshal(dto.TransferRequest{
		IdempotencyKey: "test-idempotency-key",
		FromWalletID:   "wallet1",
		ToWalletID:     "wallet2",
		Amount:         -50.0, // Negative amount to trigger validation error
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.TransferHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "from_wallet_id, to_wallet_id and a positive amount are required")
}

func TestTransferHandler_InvalidRequest_MissingFields(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	body, _ := json.Marshal(dto.TransferRequest{
		IdempotencyKey: "test-idempotency-key",
		FromWalletID:   "", // Missing from_wallet_id to trigger validation error
		ToWalletID:     "wallet2",
		Amount:         100.0,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.TransferHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "from_wallet_id, to_wallet_id and a positive amount are required")
}

func TestTransferHandler_InvalidRequest_BadJSON(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	// Invalid JSON body
	body := []byte(`{"idempotency_key": "test-idempotency-key", "from_wallet_id": "wallet1", "to_wallet_id": "wallet2",`)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.TransferHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid request body")
}

func TestTransferHandler_ServiceError(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("TransferFunds", mock.Anything, "test-idempotency-key", "wallet1", "wallet2", 100.0).Return("", "", int64(0), assert.AnError)

	body, _ := json.Marshal(dto.TransferRequest{
		IdempotencyKey: "test-idempotency-key",
		FromWalletID:   "wallet1",
		ToWalletID:     "wallet2",
		Amount:         100.0,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.TransferHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "transfer failed")

	mockWalletSvc.AssertExpectations(t)
}

func TestWalletBalanceHandler(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("GetWalletBalance", mock.Anything, "wallet1").Return("Alice", 250.5, nil)

	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet1/balance", nil).WithContext(ctx)
	req = mux.SetURLVars(req, map[string]string{"id": "wallet1"})
	rec := httptest.NewRecorder()

	handler.WalletBalanceHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp dto.WalletBalanceResponse
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "wallet1", resp.WalletID)
	assert.Equal(t, "Alice", resp.OwnerName)
	assert.Equal(t, 250.5, resp.Balance)

	mockWalletSvc.AssertExpectations(t)
}

func TestWalletBalanceHandler_NotFound(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("GetWalletBalance", mock.Anything, "missing-wallet").Return("", 0.0, domain.ErrWalletNotFound)

	req := httptest.NewRequest(http.MethodGet, "/wallets/missing-wallet/balance", nil).WithContext(ctx)
	req = mux.SetURLVars(req, map[string]string{"id": "missing-wallet"})
	rec := httptest.NewRecorder()

	handler.WalletBalanceHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "wallet not found")

	mockWalletSvc.AssertExpectations(t)
}

func TestWalletBalanceHandler_MissingID(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	req := httptest.NewRequest(http.MethodGet, "/wallets//balance", nil)
	req = mux.SetURLVars(req, map[string]string{"id": ""})
	rec := httptest.NewRecorder()

	handler.WalletBalanceHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "wallet id is required")
}

func TestTransferHistoryHandler(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("GetTransferHistory", mock.Anything, "wallet1", int32(0), int32(0)).
		Return([]*domain.Transfer{
			{ID: "txn_1", FromWalletID: "wallet1", ToWalletID: "wallet2", Amount: 100, Status: domain.TransferProcessed},
		}, int32(20), int32(0), nil)

	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet1/transfers", nil).WithContext(ctx)
	req = mux.SetURLVars(req, map[string]string{"id": "wallet1"})
	rec := httptest.NewRecorder()

	handler.TransferHistoryHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp dto.TransferHistoryResponse
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "wallet1", resp.WalletID)
	assert.Len(t, resp.Transfers, 1)
	assert.Equal(t, "txn_1", resp.Transfers[0].TransactionID)
	assert.Equal(t, "DEBIT", resp.Transfers[0].Type)
	assert.Equal(t, 20, resp.Limit)

	mockWalletSvc.AssertExpectations(t)
}

func TestTransferHistoryHandler_CreditType(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("GetTransferHistory", mock.Anything, "wallet2", int32(0), int32(0)).
		Return([]*domain.Transfer{
			{ID: "txn_1", FromWalletID: "wallet1", ToWalletID: "wallet2", Amount: 100, Status: domain.TransferProcessed},
		}, int32(20), int32(0), nil)

	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet2/transfers", nil).WithContext(ctx)
	req = mux.SetURLVars(req, map[string]string{"id": "wallet2"})
	rec := httptest.NewRecorder()

	handler.TransferHistoryHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp dto.TransferHistoryResponse
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "CREDIT", resp.Transfers[0].Type)

	mockWalletSvc.AssertExpectations(t)
}

func TestTransferHistoryHandler_WithPagination(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("GetTransferHistory", mock.Anything, "wallet1", int32(10), int32(5)).
		Return([]*domain.Transfer{}, int32(10), int32(5), nil)

	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet1/transfers?limit=10&offset=5", nil).WithContext(ctx)
	req = mux.SetURLVars(req, map[string]string{"id": "wallet1"})
	rec := httptest.NewRecorder()

	handler.TransferHistoryHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	mockWalletSvc.AssertExpectations(t)
}

func TestTransferHistoryHandler_InvalidLimit(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet1/transfers?limit=abc", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "wallet1"})
	rec := httptest.NewRecorder()

	handler.TransferHistoryHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "limit and offset must be valid integers")
}

func TestTransferHistoryHandler_MissingID(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	req := httptest.NewRequest(http.MethodGet, "/wallets//transfers", nil)
	req = mux.SetURLVars(req, map[string]string{"id": ""})
	rec := httptest.NewRecorder()

	handler.TransferHistoryHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "wallet id is required")
}

func TestTransferHistoryHandler_WalletNotFound(t *testing.T) {
	mockWalletSvc := testutils.MockIWalletSvc{}

	ctx := context.Background()
	mockWalletSvc.On("GetTransferHistory", mock.Anything, "missing-wallet", int32(0), int32(0)).
		Return(nil, int32(0), int32(0), domain.ErrWalletNotFound)

	req := httptest.NewRequest(http.MethodGet, "/wallets/missing-wallet/transfers", nil).WithContext(ctx)
	req = mux.SetURLVars(req, map[string]string{"id": "missing-wallet"})
	rec := httptest.NewRecorder()

	handler.TransferHistoryHandler(&mockWalletSvc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "wallet not found")
}
