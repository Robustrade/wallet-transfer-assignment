package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/handler"
	"github.com/example/wallet-transfer/internal/repository/memory"
	"github.com/example/wallet-transfer/internal/service"
)

func newTestHandler() *handler.TransferHandler {
	store := memory.NewStore()
	store.SeedWallet(domain.Wallet{ID: "wallet_1", Balance: 500})
	store.SeedWallet(domain.Wallet{ID: "wallet_2", Balance: 100})
	return handler.NewTransferHandler(service.NewTransferService(store))
}

func postTransfer(h *handler.TransferHandler, body map[string]interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	h.CreateTransfer(rec, req)
	return rec
}

func TestCreateTransfer_HTTP_Success(t *testing.T) {
	h := newTestHandler()
	rec := postTransfer(h, map[string]interface{}{
		"idempotencyKey": "http-1",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var result service.TransferResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("response was not valid JSON: %v", err)
	}
	if result.State != domain.TransferProcessed {
		t.Fatalf("expected PROCESSED in response body, got %s", result.State)
	}
}

func TestCreateTransfer_HTTP_InvalidJSON(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader([]byte("{not valid json")))
	rec := httptest.NewRecorder()
	h.CreateTransfer(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransfer_HTTP_InsufficientFunds(t *testing.T) {
	h := newTestHandler()
	rec := postTransfer(h, map[string]interface{}{
		"idempotencyKey": "http-2",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         9999,
	})
	// The request was fine; the business outcome wasn't — 422, not 4xx/5xx.
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTransfer_HTTP_MissingIdempotencyKey(t *testing.T) {
	h := newTestHandler()
	rec := postTransfer(h, map[string]interface{}{
		"fromWalletId": "wallet_1",
		"toWalletId":   "wallet_2",
		"amount":       10,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTransfer_HTTP_IdempotentReplaySameStatusCode(t *testing.T) {
	h := newTestHandler()
	body := map[string]interface{}{
		"idempotencyKey": "http-replay",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         20,
	}

	first := postTransfer(h, body)
	second := postTransfer(h, body)

	if first.Code != second.Code {
		t.Fatalf("replay returned a different status code: %d vs %d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replay returned a different body:\n%s\nvs\n%s", first.Body.String(), second.Body.String())
	}
}

func TestCreateTransfer_HTTP_ConflictingIdempotencyKeyReuse(t *testing.T) {
	h := newTestHandler()
	postTransfer(h, map[string]interface{}{
		"idempotencyKey": "http-conflict",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         10,
	})
	rec := postTransfer(h, map[string]interface{}{
		"idempotencyKey": "http-conflict",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         999, // different amount, same key
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}
