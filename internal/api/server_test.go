package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

func TestHandleCreateTransferReturnsCreated(t *testing.T) {
	server := newTestServer()
	body := `{
		"idempotencyKey": "key-1",
		"fromWalletId": "wallet-a",
		"toWalletId": "wallet-b",
		"amount": 100
	}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(body))
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	var response service.TransferResult
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != string(domain.TransferStatusProcessed) {
		t.Fatalf("status = %q, want %q", response.Status, domain.TransferStatusProcessed)
	}
}

func TestHandleCreateTransferReturnsOKForReplay(t *testing.T) {
	server := newTestServer()
	server.transfers.(*stubTransferService).replayed = true

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(`{
		"idempotencyKey": "key-1",
		"fromWalletId": "wallet-a",
		"toWalletId": "wallet-b",
		"amount": 100
	}`))
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestHandleCreateTransferMapsValidationError(t *testing.T) {
	server := newTestServer()
	server.transfers.(*stubTransferService).createErr = domain.ErrInvalidAmount

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(`{
		"idempotencyKey": "key-1",
		"fromWalletId": "wallet-a",
		"toWalletId": "wallet-b",
		"amount": 0
	}`))
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestHandleGetWalletReturnsNotFound(t *testing.T) {
	server := newTestServer()
	server.wallets.(*stubWalletService).walletErr = domain.ErrWalletNotFound

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/wallets/missing-wallet", nil)
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func newTestServer() *Server {
	return NewServer(
		&stubTransferService{},
		&stubWalletService{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

type stubTransferService struct {
	replayed  bool
	createErr error
	getErr    error
}

func (stub *stubTransferService) CreateTransfer(ctx context.Context, request service.CreateTransferRequest) (service.CreateTransferResult, error) {
	if stub.createErr != nil {
		return service.CreateTransferResult{}, stub.createErr
	}
	return service.CreateTransferResult{
		Replayed: stub.replayed,
		Transfer: service.TransferResult{
			TransferID:     "transfer-1",
			IdempotencyKey: request.IdempotencyKey,
			Status:         string(domain.TransferStatusProcessed),
			FromWalletID:   request.FromWalletID,
			ToWalletID:     request.ToWalletID,
			Amount:         request.Amount,
			Currency:       domain.NormalizeCurrency(request.Currency),
			CreatedAt:      "2026-07-19T00:00:00Z",
		},
	}, nil
}

func (stub *stubTransferService) GetTransfer(ctx context.Context, id string) (service.TransferResult, error) {
	if stub.getErr != nil {
		return service.TransferResult{}, stub.getErr
	}
	return service.TransferResult{TransferID: id, Status: string(domain.TransferStatusProcessed)}, nil
}

type stubWalletService struct {
	walletErr error
	ledgerErr error
}

func (stub *stubWalletService) GetWallet(ctx context.Context, id string) (service.WalletResult, error) {
	if stub.walletErr != nil {
		return service.WalletResult{}, stub.walletErr
	}
	return service.WalletResult{WalletID: id, OwnerID: "owner-1", Currency: "USD", Balance: 100, Status: "ACTIVE"}, nil
}

func (stub *stubWalletService) ListLedger(ctx context.Context, walletID string, limit int, cursor int64) ([]service.LedgerEntryResult, error) {
	if stub.ledgerErr != nil {
		return nil, stub.ledgerErr
	}
	return []service.LedgerEntryResult{}, nil
}
