package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/raknay/wallet-transfer-assignment/internal/wallet/domain"
	mock_service "github.com/raknay/wallet-transfer-assignment/internal/wallet/service/mock"
	"go.uber.org/mock/gomock"
)

func TestCreateTransfer_InvalidBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	service := mock_service.NewMockWalletService(ctrl)

	router := chi.NewRouter()
	NewWalletHandler(router, service)

	req := httptest.NewRequest(http.MethodPost, "/wallet/transfer", bytes.NewBufferString("{invalid-json"))
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	service := mock_service.NewMockWalletService(ctrl)

	router := chi.NewRouter()
	NewWalletHandler(router, service)

	request := domain.Transfer{
		IdempotencyKey: "idmKey1",
		FromWallet:     "wallet-1",
		ToWallet:       "wallet-2",
		Amount:         100,
		Status:         "PENDING",
	}

	expected := domain.Transfer{
		ID:             "uuid-1",
		IdempotencyKey: request.IdempotencyKey,
		FromWallet:     request.FromWallet,
		ToWallet:       request.ToWallet,
		Amount:         request.Amount,
		Status:         "PROCESSED",
	}

	service.EXPECT().CreateTransfer(gomock.Any(), request).Return(expected, nil)

	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/wallet/transfer", bytes.NewReader(payload))
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, res.Code)
	}

	var actual domain.Transfer
	if err := json.NewDecoder(res.Body).Decode(&actual); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if actual != expected {
		t.Fatalf("expected %#v, got %#v", expected, actual)
	}
}
