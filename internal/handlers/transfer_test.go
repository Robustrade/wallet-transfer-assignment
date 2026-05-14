package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"github.com/manish8561/wallet-transfer-assignment/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockTransferService is a mock for TransferService
type MockTransferService struct {
	mock.Mock
}

func (m *MockTransferService) ExecuteTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*services.TransferResponse, error) {
	args := m.Called(ctx, idempotencyKey, fromWalletID, toWalletID, amount)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.TransferResponse), args.Error(1)
}

func (m *MockTransferService) GetBalance(ctx context.Context, walletID string) (int64, error) {
	args := m.Called(ctx, walletID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockTransferService) GetTransferHistory(ctx context.Context, walletID string, limit, offset int) ([]*models.Transfer, error) {
	args := m.Called(ctx, walletID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Transfer), args.Error(1)
}

func (m *MockTransferService) CreateWallet(ctx context.Context, initialBalance int64) (*models.Wallet, error) {
	args := m.Called(ctx, initialBalance)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Wallet), args.Error(1)
}

func TestTransferHandler_CreateTransfer(t *testing.T) {
	mockService := new(MockTransferService)
	handler := NewTransferHandler(mockService)

	t.Run("successful transfer", func(t *testing.T) {
		expectedResponse := &services.TransferResponse{
			TransferID:   "transfer-123",
			Status:       "PROCESSED",
			FromWalletID: "wallet-1",
			ToWalletID:   "wallet-2",
			Amount:       100,
		}

		mockService.On("ExecuteTransfer", mock.Anything, "key-1", "wallet-1", "wallet-2", int64(100)).
			Return(expectedResponse, nil).Once()

		reqBody := map[string]interface{}{
			"idempotencyKey": "key-1",
			"fromWalletId":   "wallet-1",
			"toWalletId":     "wallet-2",
			"amount":         100,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.CreateTransfer(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp services.TransferResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "transfer-123", resp.TransferID)
		assert.Equal(t, "PROCESSED", resp.Status)

		mockService.AssertExpectations(t)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader([]byte("invalid json")))
		w := httptest.NewRecorder()

		handler.CreateTransfer(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var resp ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Contains(t, resp.Error, "invalid request body")
	})

	t.Run("missing required fields", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"fromWalletId": "wallet-1",
			"toWalletId":   "wallet-2",
			"amount":       100,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.CreateTransfer(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var resp ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Contains(t, resp.Error, "missing required fields")
	})

	t.Run("missing idempotency key", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"fromWalletId": "wallet-1",
			"toWalletId":   "wallet-2",
			"amount":       100,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.CreateTransfer(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing from wallet ID", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"idempotencyKey": "key-1",
			"toWalletId":     "wallet-2",
			"amount":         100,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.CreateTransfer(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing to wallet ID", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"idempotencyKey": "key-1",
			"fromWalletId":   "wallet-1",
			"amount":         100,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.CreateTransfer(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error - insufficient balance", func(t *testing.T) {
		mockService.On("ExecuteTransfer", mock.Anything, "key-1", "wallet-1", "wallet-2", int64(100)).
			Return(nil, errors.New("insufficient balance: have 50, need 100")).Once() // Use a specific error message

		reqBody := map[string]interface{}{
			"idempotencyKey": "key-1",
			"fromWalletId":   "wallet-1",
			"toWalletId":     "wallet-2",
			"amount":         100,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler.CreateTransfer(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code) // Expect 422

		var resp ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Contains(t, resp.Error, "insufficient balance")

		mockService.AssertExpectations(t)
	})

	t.Run("service error - generic internal error", func(t *testing.T) {
		mockService.On("ExecuteTransfer", mock.Anything, "key-2", "wallet-3", "wallet-4", int64(50)).
			Return(nil, errors.New("some unexpected internal error")).Once()

		reqBody := map[string]interface{}{
			"idempotencyKey": "key-2",
			"fromWalletId":   "wallet-3",
			"toWalletId":     "wallet-4",
			"amount":         50,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/transfers", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler.CreateTransfer(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		var resp ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Contains(t, resp.Error, "some unexpected internal error")

		mockService.AssertExpectations(t)
	})
}

func TestTransferHandler_GetBalance(t *testing.T) {
	mockService := new(MockTransferService)
	handler := NewTransferHandler(mockService)

	router := http.NewServeMux()
	router.HandleFunc("GET /wallets/balance/{wallet_id}", handler.GetBalance)

	t.Run("successful balance retrieval", func(t *testing.T) {
		mockService.On("GetBalance", mock.Anything, "wallet-1").Return(int64(500), nil).Once()

		req := httptest.NewRequest("GET", "/wallets/balance/wallet-1", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "wallet-1", resp["wallet_id"])
		assert.Equal(t, float64(500), resp["balance"])

		mockService.AssertExpectations(t)
	})

	t.Run("service error", func(t *testing.T) {
		mockService.On("GetBalance", mock.Anything, "wallet-1").Return(int64(0), assert.AnError).Once()

		req := httptest.NewRequest("GET", "/wallets/balance/wallet-1", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)

		mockService.AssertExpectations(t)
	})
}

func TestCreateTransferRequest(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		req := CreateTransferRequest{
			IdempotencyKey: "key-1",
			FromWalletID:   "wallet-1",
			ToWalletID:     "wallet-2",
			Amount:         100,
		}

		assert.Equal(t, "key-1", req.IdempotencyKey)
		assert.Equal(t, "wallet-1", req.FromWalletID)
		assert.Equal(t, "wallet-2", req.ToWalletID)
		assert.Equal(t, int64(100), req.Amount)
	})
}

func TestErrorResponse(t *testing.T) {
	t.Run("error response structure", func(t *testing.T) {
		resp := ErrorResponse{
			Error: "test error message",
		}

		assert.Equal(t, "test error message", resp.Error)
	})
}

func TestNewTransferHandler(t *testing.T) {
	mockService := new(MockTransferService)
	handler := NewTransferHandler(mockService)

	assert.NotNil(t, handler)
	assert.Equal(t, mockService, handler.service)
}
