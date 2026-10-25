package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/service"
	transporthttp "github.com/dale-tomson/wallet-transfer-assignment/internal/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockWalletService struct {
	mock.Mock
}

func (m *MockWalletService) CreateWallet(ctx context.Context, userID, currency string) (*domain.Wallet, error) {
	args := m.Called(ctx, userID, currency)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletService) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	args := m.Called(ctx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletService) Transfer(ctx context.Context, req service.TransferRequest) (*service.TransferResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TransferResponse), args.Error(1)
}

func TestHealthEndpoint(t *testing.T) {
	handler := transporthttp.NewWalletHandler(new(MockWalletService))
	router := transporthttp.RegisterRoutes(handler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body["status"])
}

func TestWalletHandler_CreateWallet_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		setupMock      func(m *MockWalletService)
		expectedStatus int
	}{
		{
			name: "success: valid request creates wallet",
			body: `{"userId":"user_1","currency":"usd"}`,
			setupMock: func(m *MockWalletService) {
				m.On("CreateWallet", mock.Anything, "user_1", "USD").Return(&domain.Wallet{
					ID:        "w_1",
					UserID:    "user_1",
					Currency:  "USD",
					Balance:   0,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}, nil)
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "failure: missing userId",
			body:           `{"userId":"","currency":"USD"}`,
			setupMock:      func(m *MockWalletService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "failure: missing currency",
			body:           `{"userId":"user_1","currency":""}`,
			setupMock:      func(m *MockWalletService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "failure: invalid json body",
			body:           `{invalid-json`,
			setupMock:      func(m *MockWalletService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "failure: service returns error",
			body: `{"userId":"user_1","currency":"USD"}`,
			setupMock: func(m *MockWalletService) {
				m.On("CreateWallet", mock.Anything, "user_1", "USD").Return((*domain.Wallet)(nil), errors.New("db failure"))
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(MockWalletService)
			tt.setupMock(svc)

			handler := transporthttp.NewWalletHandler(svc)
			router := transporthttp.RegisterRoutes(handler)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			svc.AssertExpectations(t)
		})
	}
}

func TestWalletHandler_GetWallet_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		walletID       string
		setupMock      func(m *MockWalletService)
		expectedStatus int
	}{
		{
			name:     "success: wallet exists",
			walletID: "w_123",
			setupMock: func(m *MockWalletService) {
				m.On("GetWallet", mock.Anything, "w_123").Return(&domain.Wallet{
					ID:       "w_123",
					UserID:   "u1",
					Currency: "USD",
					Balance:  1000,
				}, nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:     "failure: wallet not found",
			walletID: "w_missing",
			setupMock: func(m *MockWalletService) {
				m.On("GetWallet", mock.Anything, "w_missing").Return((*domain.Wallet)(nil), domain.ErrWalletNotFound)
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(MockWalletService)
			tt.setupMock(svc)

			handler := transporthttp.NewWalletHandler(svc)
			router := transporthttp.RegisterRoutes(handler)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/wallets/"+tt.walletID, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			svc.AssertExpectations(t)
		})
	}
}

func TestWalletHandler_Transfer_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		headerKey      string
		setupMock      func(m *MockWalletService)
		expectedStatus int
	}{
		{
			name: "success: valid transfer from body idempotencyKey",
			body: `{"idempotencyKey":"key-1","fromWalletId":"w1","toWalletId":"w2","amount":500}`,
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, service.TransferRequest{
					IdempotencyKey: "key-1",
					FromWalletID:   "w1",
					ToWalletID:     "w2",
					Amount:         500,
				}).Return(&service.TransferResponse{
					TransferID: "tx-1",
					Status:     "COMPLETED",
					Amount:     500,
					Currency:   "USD",
					CreatedAt:  time.Now().Format(time.RFC3339),
				}, nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:      "success: header fallback for idempotency key",
			body:      `{"fromWalletId":"w1","toWalletId":"w2","amount":500}`,
			headerKey: "header-key-fallback",
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, service.TransferRequest{
					IdempotencyKey: "header-key-fallback",
					FromWalletID:   "w1",
					ToWalletID:     "w2",
					Amount:         500,
				}).Return(&service.TransferResponse{
					TransferID: "tx-2",
					Status:     "COMPLETED",
					Amount:     500,
					Currency:   "USD",
				}, nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "failure: missing idempotency key entirely",
			body:           `{"fromWalletId":"w1","toWalletId":"w2","amount":500}`,
			headerKey:      "",
			setupMock:      func(m *MockWalletService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "failure: insufficient funds maps to 422",
			body: `{"idempotencyKey":"key-insuf","fromWalletId":"w1","toWalletId":"w2","amount":5000}`,
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, mock.Anything).Return((*service.TransferResponse)(nil), domain.ErrInsufficientFunds)
			},
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "failure: concurrent request maps to 409",
			body: `{"idempotencyKey":"key-concur","fromWalletId":"w1","toWalletId":"w2","amount":100}`,
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, mock.Anything).Return((*service.TransferResponse)(nil), domain.ErrConcurrentRequest)
			},
			expectedStatus: http.StatusConflict,
		},
		{
			name: "failure: idempotency key conflict maps to 422",
			body: `{"idempotencyKey":"key-conflict","fromWalletId":"w1","toWalletId":"w2","amount":100}`,
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, mock.Anything).Return((*service.TransferResponse)(nil), domain.ErrIdempotencyKeyConflict)
			},
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "failure: currency mismatch maps to 400",
			body: `{"idempotencyKey":"key-curr","fromWalletId":"w1","toWalletId":"w2","amount":100}`,
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, mock.Anything).Return((*service.TransferResponse)(nil), domain.ErrCurrencyMismatch)
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "failure: self transfer maps to 400",
			body: `{"idempotencyKey":"key-self","fromWalletId":"w1","toWalletId":"w1","amount":100}`,
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, mock.Anything).Return((*service.TransferResponse)(nil), domain.ErrSelfTransfer)
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "failure: wallet not found maps to 404",
			body: `{"idempotencyKey":"key-missing","fromWalletId":"w_miss","toWalletId":"w2","amount":100}`,
			setupMock: func(m *MockWalletService) {
				m.On("Transfer", mock.Anything, mock.Anything).Return((*service.TransferResponse)(nil), domain.ErrWalletNotFound)
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(MockWalletService)
			tt.setupMock(svc)

			handler := transporthttp.NewWalletHandler(svc)
			router := transporthttp.RegisterRoutes(handler)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", bytes.NewBufferString(tt.body))
			if tt.headerKey != "" {
				req.Header.Set("Idempotency-Key", tt.headerKey)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			svc.AssertExpectations(t)
		})
	}
}
