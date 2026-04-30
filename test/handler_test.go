package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gerry1818/wallet-transfer-assignment/internal/handler"
	"github.com/gerry1818/wallet-transfer-assignment/internal/logger"
	"github.com/gerry1818/wallet-transfer-assignment/internal/model/dto"
	"github.com/gerry1818/wallet-transfer-assignment/internal/service"
)

// TestHealthEndpoint tests the health check endpoint
func TestHealthEndpoint(t *testing.T) {
	logger.Init()
	repo := &MockRepo{claimOK: true}
	svc := service.NewTransferService(repo)
	h := handler.NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var response handler.HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("expected status 'healthy', got '%s'", response.Status)
	}
}

// TestTransferHandlerSuccess tests successful transfer via HTTP handler
func TestTransferHandlerSuccess(t *testing.T) {
	logger.Init()
	repo := &MockRepo{claimOK: true}
	svc := service.NewTransferService(repo)
	h := handler.NewHandler(svc)

	reqBody := dto.TransferRequestDTO{
		IdempotencyKey: "test-1",
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         100,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Transfer(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var response dto.TransferResponseDTO
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Status != "PROCESSED" {
		t.Errorf("expected status PROCESSED, got %s", response.Status)
	}
}

// TestTransferHandlerInvalidMethod tests invalid HTTP method
func TestTransferHandlerInvalidMethod(t *testing.T) {
	logger.Init()
	repo := &MockRepo{claimOK: true}
	svc := service.NewTransferService(repo)
	h := handler.NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	w := httptest.NewRecorder()

	h.Transfer(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// TestTransferHandlerInvalidJSON tests invalid JSON body
func TestTransferHandlerInvalidJSON(t *testing.T) {
	logger.Init()
	repo := &MockRepo{claimOK: true}
	svc := service.NewTransferService(repo)
	h := handler.NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader([]byte("invalid")))
	w := httptest.NewRecorder()

	h.Transfer(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestHealthEndpointInvalidMethod tests invalid HTTP method for health
func TestHealthEndpointInvalidMethod(t *testing.T) {
	logger.Init()
	repo := &MockRepo{claimOK: true}
	svc := service.NewTransferService(repo)
	h := handler.NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
