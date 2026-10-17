package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// mockTransferService implements service.TransferService for handler tests.
type mockTransferService struct {
	createTransferFunc func(ctx context.Context, req *service.CreateTransferRequest) (*service.TransferResult, error)
	getTransferFunc    func(ctx context.Context, id string) (*domain.Transfer, error)
	getWalletBalFunc   func(ctx context.Context, walletID string) (*service.WalletBalanceResult, error)
}

func (m *mockTransferService) CreateTransfer(ctx context.Context, req *service.CreateTransferRequest) (*service.TransferResult, error) {
	if m.createTransferFunc != nil {
		return m.createTransferFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockTransferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	if m.getTransferFunc != nil {
		return m.getTransferFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockTransferService) GetWalletBalance(ctx context.Context, walletID string) (*service.WalletBalanceResult, error) {
	if m.getWalletBalFunc != nil {
		return m.getWalletBalFunc(ctx, walletID)
	}
	return nil, nil
}

func TestTransferHandler_Validation(t *testing.T) {
	mockSvc := &mockTransferService{}
	h := handler.NewTransferHandler(mockSvc)

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "reject unknown fields",
			body:           `{"idempotencyKey":"key1","fromWalletId":"w1","toWalletId":"w2","amount":100,"extra":"bad"}`,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   domain.CodeInvalidRequest,
		},
		{
			name:           "reject malformed JSON",
			body:           `{"idempotencyKey":`,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   domain.CodeInvalidRequest,
		},
		{
			name:           "reject empty body",
			body:           ``,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   domain.CodeInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleCreateTransfer(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			var errResp handler.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("failed to parse error response: %v", err)
			}
			if errResp.Error.Code != tt.expectedCode {
				t.Errorf("expected error code %s, got %s", tt.expectedCode, errResp.Error.Code)
			}
		})
	}
}

func TestTransferHandler_SuccessAndReplay(t *testing.T) {
	successPayload := `{"id":"trn_123","status":"PROCESSED","amount":5000}`
	mockSvc := &mockTransferService{
		createTransferFunc: func(ctx context.Context, req *service.CreateTransferRequest) (*service.TransferResult, error) {
			if req.IdempotencyKey == "replay-key" {
				return &service.TransferResult{
					StatusCode:   http.StatusCreated,
					ResponseBody: []byte(successPayload),
					Replayed:     true,
				}, nil
			}
			return &service.TransferResult{
				StatusCode:   http.StatusCreated,
				ResponseBody: []byte(successPayload),
				Replayed:     false,
			}, nil
		},
	}
	h := handler.NewTransferHandler(mockSvc)

	// 1. Initial creation
	reqBody := `{"idempotencyKey":"new-key","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":5000}`
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	h.HandleCreateTransfer(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}
	if w.Header().Get("X-Idempotent-Replayed") == "true" {
		t.Errorf("expected X-Idempotent-Replayed to not be set for fresh transfer")
	}

	// 2. Replayed request
	reqBodyReplay := `{"idempotencyKey":"replay-key","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":5000}`
	req2 := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(reqBodyReplay))
	w2 := httptest.NewRecorder()

	h.HandleCreateTransfer(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w2.Code)
	}
	if w2.Header().Get("X-Idempotent-Replayed") != "true" {
		t.Errorf("expected X-Idempotent-Replayed to be true for replayed transfer")
	}
}

func TestWalletHandler_GetBalance(t *testing.T) {
	mockSvc := &mockTransferService{
		getWalletBalFunc: func(ctx context.Context, walletID string) (*service.WalletBalanceResult, error) {
			if walletID == "wallet_1" {
				return &service.WalletBalanceResult{
					WalletID:  "wallet_1",
					UserID:    "user_1",
					Balance:   1000000,
					Currency:  "INR",
					UpdatedAt: time.Now(),
				}, nil
			}
			return nil, domain.ErrWalletNotFound
		},
	}

	h := handler.NewWalletHandler(mockSvc)

	// Valid wallet
	mux := http.NewServeMux()
	mux.HandleFunc("GET /wallets/{id}/balance", h.HandleGetBalance)

	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet_1/balance", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var res service.WalletBalanceResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode balance response: %v", err)
	}
	if res.Balance != 1000000 {
		t.Errorf("expected balance 1000000, got %d", res.Balance)
	}

	// Non-existent wallet
	reqMissing := httptest.NewRequest(http.MethodGet, "/wallets/missing/balance", nil)
	wMissing := httptest.NewRecorder()
	mux.ServeHTTP(wMissing, reqMissing)

	if wMissing.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for missing wallet, got %d", wMissing.Code)
	}
}
