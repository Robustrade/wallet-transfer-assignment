package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallet-service/internal/domain"
	"wallet-service/internal/handler"
)

// MockTransferService implements service.TransferService for testing
type MockTransferService struct {
	TransferFunc func(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*domain.Transfer, error)
}

func (m *MockTransferService) Transfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*domain.Transfer, error) {
	if m.TransferFunc != nil {
		return m.TransferFunc(ctx, idempotencyKey, fromWalletID, toWalletID, amount)
	}
	return nil, nil
}

func TestTransferHandler_HandleTransfer(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		mockSetup      func(*MockTransferService)
		expectedStatus int
	}{
		{
			name: "Success",
			requestBody: map[string]interface{}{
				"idempotencyKey": "test-key",
				"fromWalletId":   "wallet_1",
				"toWalletId":     "wallet_2",
				"amount":         100,
			},
			mockSetup: func(m *MockTransferService) {
				m.TransferFunc = func(ctx context.Context, idempotencyKey, from, to string, amount int64) (*domain.Transfer, error) {
					return &domain.Transfer{
						ID:             "trans-123",
						IdempotencyKey: idempotencyKey,
						State:          domain.TransferStateProcessed,
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "Missing Idempotency Key",
			requestBody: map[string]interface{}{
				"fromWalletId": "wallet_1",
				"toWalletId":   "wallet_2",
				"amount":       100,
			},
			mockSetup: func(m *MockTransferService) {
				// Should not be called
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "Invalid JSON",
			requestBody: nil, // We'll send raw garbage text
			mockSetup: func(m *MockTransferService) {
				// Should not be called
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Domain Error (Insufficient Funds)",
			requestBody: map[string]interface{}{
				"idempotencyKey": "test-key",
				"fromWalletId":   "wallet_1",
				"toWalletId":     "wallet_2",
				"amount":         1000000,
			},
			mockSetup: func(m *MockTransferService) {
				m.TransferFunc = func(ctx context.Context, idempotencyKey, from, to string, amount int64) (*domain.Transfer, error) {
					return nil, domain.ErrInsufficientFunds
				}
			},
			expectedStatus: http.StatusBadRequest, // Proves domain errors are mapped to 400
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := &MockTransferService{}
			tt.mockSetup(mockService)

			// We pass nil for repo because the handler only directly uses the service in this simple implementation
			h := handler.NewTransferHandler(mockService, nil)

			var reqBody []byte
			if tt.name == "Invalid JSON" {
				reqBody = []byte(`{invalid-json`)
			} else {
				reqBody, _ = json.Marshal(tt.requestBody)
			}

			req, _ := http.NewRequest("POST", "/transfers", bytes.NewBuffer(reqBody))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			h.HandleTransfer(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tt.expectedStatus)
			}
		})
	}
}
