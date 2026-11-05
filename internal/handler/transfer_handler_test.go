package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type transferServiceStub struct {
	transfer *domain.Transfer
	err      error
	input    service.CreateTransferInput
}

func (s *transferServiceStub) CreateTransfer(_ context.Context, input service.CreateTransferInput) (*domain.Transfer, error) {
	s.input = input
	return s.transfer, s.err
}

func TestTransferHandlerCreatesTransfer(t *testing.T) {
	stub := &transferServiceStub{transfer: &domain.Transfer{ID: "transfer-1", Status: domain.TransferStatusProcessed}}
	mux := http.NewServeMux()
	NewTransferHandler(stub).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/transfers",
		strings.NewReader(`{"idempotencyKey":"request-1","fromWalletId":"wallet-1","toWalletId":"wallet-2","amount":75}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if stub.input.IdempotencyKey != "request-1" || stub.input.FromWalletID != "wallet-1" ||
		stub.input.ToWalletID != "wallet-2" || stub.input.Amount != 75 {
		t.Fatalf("unexpected service input: %+v", stub.input)
	}
}

func TestTransferHandlerMapsInsufficientFunds(t *testing.T) {
	stub := &transferServiceStub{err: domain.ErrInsufficientFunds}
	mux := http.NewServeMux()
	NewTransferHandler(stub).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/transfers",
		strings.NewReader(`{"idempotencyKey":"request-1","fromWalletId":"wallet-1","toWalletId":"wallet-2","amount":75}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}
