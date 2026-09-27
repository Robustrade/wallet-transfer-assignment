package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTransferInvalidJSON(t *testing.T) {
	h := &TransferHandler{}

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		strings.NewReader(`invalid-json`),
	)

	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestCreateTransferMissingFields(t *testing.T) {
	h := &TransferHandler{}

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		strings.NewReader(`{
			"idempotencyKey": "",
			"fromWalletId": "wallet_1",
			"toWalletId": "wallet_2",
			"amount": 100
		}`),
	)

	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}
