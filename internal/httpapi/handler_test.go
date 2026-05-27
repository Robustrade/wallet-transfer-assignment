package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/httpapi"
)

func TestCreateTransferRejectsTrailingJSONTokens(t *testing.T) {
	service := &stubTransferService{}
	handler := httpapi.NewHandler(service).Routes()
	request := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(`{
		"idempotencyKey": "key",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 100
	} {}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("service should not be called for malformed JSON bodies")
	}
}

func TestCreateTransferRejectsOversizedRequestBody(t *testing.T) {
	service := &stubTransferService{}
	handler := httpapi.NewHandler(service).Routes()
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"padding":"`+strings.Repeat("x", 1<<20)+`"}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	if service.called {
		t.Fatal("service should not be called for oversized request bodies")
	}
}

type stubTransferService struct {
	called bool
}

func (s *stubTransferService) CreateTransfer(context.Context, domain.TransferCommand) (domain.Outcome, error) {
	s.called = true
	return domain.Outcome{
		StatusCode: http.StatusCreated,
		Body:       domain.APIResponse{},
	}, nil
}
