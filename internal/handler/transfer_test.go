package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/repository/memory"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/service"
)

type stubTransferService struct{}

func (stubTransferService) CreateTransfer(context.Context, service.CreateTransferInput) (*service.CreateTransferResult, error) {
	return nil, service.ErrNotImplemented
}

func TestCreateTransfer_validation(t *testing.T) {
	h := NewTransferHandler(stubTransferService{})

	body := []byte(`{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w1","amount":10}`)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransfer_notImplemented(t *testing.T) {
	h := NewTransferHandler(stubTransferService{})

	body := []byte(`{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":10}`)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestCreateTransfer_success(t *testing.T) {
	store := memory.NewStore()
	store.SeedWallet(&domain.Wallet{ID: "wallet_1", UserID: "u1", Balance: 500, Currency: "USD"})
	store.SeedWallet(&domain.Wallet{ID: "wallet_2", UserID: "u2", Balance: 100, Currency: "USD"})

	h := NewTransferHandler(service.NewTransferService(store))

	body := []byte(`{"idempotencyKey":"api-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":50}`)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp createTransferResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TransferID == "" || resp.State != string(domain.TransferStateProcessed) {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateTransfer_insufficientFunds(t *testing.T) {
	store := memory.NewStore()
	store.SeedWallet(&domain.Wallet{ID: "wallet_1", UserID: "u1", Balance: 10, Currency: "USD"})
	store.SeedWallet(&domain.Wallet{ID: "wallet_2", UserID: "u2", Balance: 0, Currency: "USD"})

	h := NewTransferHandler(service.NewTransferService(store))

	body := []byte(`{"idempotencyKey":"api-fail","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestHealthHandler(t *testing.T) {
	h := NewHealthHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
