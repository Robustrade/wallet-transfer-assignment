package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type mockTransferService struct {
	transferFunc func(ctx context.Context, req service.TransferRequest) (*model.Transfer, error)
}

func (m *mockTransferService) Transfer(ctx context.Context, req service.TransferRequest) (*model.Transfer, error) {
	if m.transferFunc != nil {
		return m.transferFunc(ctx, req)
	}
	return nil, errors.New("unimplemented")
}

func TestTransferHandler_CreateTransfer(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		body           interface{}
		mockService    func(ctx context.Context, req service.TransferRequest) (*model.Transfer, error)
		expectedStatus int
	}{
		{
			name:   "method not allowed",
			method: http.MethodGet,
			body:   nil,
			mockService: func(ctx context.Context, req service.TransferRequest) (*model.Transfer, error) {
				return nil, nil // Should not be reached
			},
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "invalid json",
			method: http.MethodPost,
			body:   "invalid_json_string", // Malformed JSON
			mockService: func(ctx context.Context, req service.TransferRequest) (*model.Transfer, error) {
				return nil, nil // Should not be reached
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "invalid request from service",
			method: http.MethodPost,
			body: service.TransferRequest{
				IdempotencyKey: "key_1",
				FromWalletID:   "w1",
				ToWalletID:     "w1", // self transfer
				Amount:         100,
			},
			mockService: func(ctx context.Context, req service.TransferRequest) (*model.Transfer, error) {
				return nil, service.ErrInvalidRequest
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "successful transfer",
			method: http.MethodPost,
			body: service.TransferRequest{
				IdempotencyKey: "key_2",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			mockService: func(ctx context.Context, req service.TransferRequest) (*model.Transfer, error) {
				return &model.Transfer{
					ID:             "t1",
					IdempotencyKey: "key_2",
					Amount:         100,
					Status:         model.StatusProcessed,
					CreatedAt:      time.Now(),
					UpdatedAt:      time.Now(),
				}, nil
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockTransferService{transferFunc: tt.mockService}
			h := NewTransferHandler(svc)

			var reqBody []byte
			var err error
			if strBody, ok := tt.body.(string); ok {
				reqBody = []byte(strBody)
			} else if tt.body != nil {
				reqBody, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("failed to marshal request body: %v", err)
				}
			}

			req, err := http.NewRequest(tt.method, "/transfers", bytes.NewBuffer(reqBody))
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			rr := httptest.NewRecorder()
			h.CreateTransfer(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tt.expectedStatus)
			}
		})
	}
}
