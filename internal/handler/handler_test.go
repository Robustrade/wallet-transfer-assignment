package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

// fakeTransferSvc lets individual tests control what each method returns.
type fakeTransferSvc struct {
	createWallet    domain.Wallet
	createWalletErr error
	executeResp     service.TransferResponse
	executeErr      error
	executeCount    int
	magicResp       service.TransferResponse
	magicErr        error
	magicCount      int
	getTransfer     domain.Transfer
	getTransErr     error
	getWallet       domain.Wallet
	getWalErr       error
}

func (f *fakeTransferSvc) CreateWallet(_ context.Context) (domain.Wallet, error) {
	return f.createWallet, f.createWalletErr
}

func (f *fakeTransferSvc) Execute(_ context.Context, _ service.TransferRequest) (service.TransferResponse, error) {
	f.executeCount++
	resp := f.executeResp
	// Simulate idempotency cache hit on second-and-subsequent calls.
	if f.executeCount > 1 {
		resp.FromCache = true
	}

	return resp, f.executeErr
}

func (f *fakeTransferSvc) Magic(_ context.Context, _ service.MagicRequest) (service.TransferResponse, error) {
	f.magicCount++
	resp := f.magicResp
	if f.magicCount > 1 {
		resp.FromCache = true
	}

	return resp, f.magicErr
}

func (f *fakeTransferSvc) GetTransfer(_ context.Context, _ uuid.UUID) (domain.Transfer, error) {
	return f.getTransfer, f.getTransErr
}

func (f *fakeTransferSvc) GetWallet(_ context.Context, _ uuid.UUID) (domain.Wallet, error) {
	return f.getWallet, f.getWalErr
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newRouter(svc handler.TransferSvc) http.Handler {
	r := chi.NewRouter()
	h := handler.New(svc)
	h.Register(r)

	return r
}

func toJSON(t *testing.T, v any) []byte {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return b
}

// ── POST /wallets ─────────────────────────────────────────────────────────────

func TestHandler_CreateWallet_201(t *testing.T) {
	t.Parallel()

	walletID := domain.NewID()
	svc := &fakeTransferSvc{
		createWallet: domain.Wallet{ID: walletID, Balance: 0},
	}

	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body)
	}

	var resp handler.WalletResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != walletID {
		t.Fatalf("wallet ID mismatch: got %s, want %s", resp.ID, walletID)
	}

	if resp.Balance != 0 {
		t.Fatalf("expected zero balance on creation, got %d", resp.Balance)
	}
}

func TestHandler_CreateWallet_500_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{createWalletErr: errors.New("db: connection lost")}

	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ── POST /transfers ───────────────────────────────────────────────────────────

func TestHandler_CreateTransfer_201(t *testing.T) {
	t.Parallel()

	transferID := domain.NewID()
	svc := &fakeTransferSvc{
		executeResp: service.TransferResponse{
			TransferID: transferID,
			Status:     domain.TransferStatusProcessed,
			Amount:     100_00,
		},
	}

	body := handler.CreateTransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body)
	}

	var resp handler.CreateTransferResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != transferID {
		t.Fatalf("transfer ID mismatch: got %s, want %s", resp.ID, transferID)
	}
}

func TestHandler_CreateTransfer_200_Idempotent(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{
		executeResp: service.TransferResponse{
			TransferID: domain.NewID(),
			Status:     domain.TransferStatusProcessed,
		},
	}
	router := newRouter(svc) // shared router so executeCount accumulates

	body := handler.CreateTransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	// First call → 201 (new transfer).
	req1 := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d", rec1.Code)
	}

	// Second call with same key → 200 (idempotent replay).
	req2 := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d", rec2.Code)
	}
}

func TestHandler_CreateTransfer_400_MissingIdempotencyKey(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}
	body := handler.CreateTransferRequest{
		FromWalletID: domain.NewID(),
		ToWalletID:   domain.NewID(),
		Amount:       100_00,
		// IdempotencyKey intentionally omitted
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_400_InvalidAmount(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{executeErr: domain.ErrInvalidAmount}
	body := handler.CreateTransferRequest{
		IdempotencyKey: "key",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         0,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_422_InsufficientFunds(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{executeErr: domain.ErrInsufficientFunds}
	body := handler.CreateTransferRequest{
		IdempotencyKey: "key",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         999_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_404_WalletNotFound(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{executeErr: domain.ErrWalletNotFound}
	body := handler.CreateTransferRequest{
		IdempotencyKey: "key",
		FromWalletID:   uuid.New(),
		ToWalletID:     uuid.New(),
		Amount:         100_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ── GET /wallets/{id} ─────────────────────────────────────────────────────────

func TestHandler_GetWallet_200(t *testing.T) {
	t.Parallel()

	walletID := domain.NewID()
	svc := &fakeTransferSvc{
		getWallet: domain.Wallet{ID: walletID, Balance: 250_00},
	}

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+walletID.String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
}

func TestHandler_GetWallet_404(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{getWalErr: domain.ErrWalletNotFound}

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ── GET /transfers/{id} ───────────────────────────────────────────────────────

func TestHandler_GetTransfer_200(t *testing.T) {
	t.Parallel()

	transferID := domain.NewID()
	svc := &fakeTransferSvc{
		getTransfer: domain.Transfer{ID: transferID, Status: domain.TransferStatusProcessed},
	}

	req := httptest.NewRequest(http.MethodGet, "/transfers/"+transferID.String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
}

func TestHandler_GetTransfer_404(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{getTransErr: domain.ErrTransferNotFound}

	req := httptest.NewRequest(http.MethodGet, "/transfers/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_400_BadJSON(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader([]byte(`{bad json}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_400_IdempotencyKeyTooLong(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}

	// Build a key that exceeds the 255-char limit.
	key := make([]byte, service.MaxIdempotencyKeyLength+1)
	for i := range key {
		key[i] = 'a'
	}

	body := handler.CreateTransferRequest{
		IdempotencyKey: string(key),
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong idempotency key, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_409_DuplicateIdempotencyKey(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{executeErr: domain.ErrDuplicateIdempotencyKey}
	body := handler.CreateTransferRequest{
		IdempotencyKey: "conflict-key",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate idempotency key, got %d", rec.Code)
	}
}

// ── POST /magic ───────────────────────────────────────────────────────────────

func TestHandler_MagicDeposit_201(t *testing.T) {
	t.Parallel()

	transferID := domain.NewID()
	svc := &fakeTransferSvc{
		magicResp: service.TransferResponse{
			TransferID: transferID,
			Status:     domain.TransferStatusProcessed,
			Amount:     5_000,
		},
	}

	body := handler.MagicDepositRequest{
		IdempotencyKey: "magic-key-001",
		ToWalletID:     domain.NewID(),
	}

	req := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body)
	}

	var resp handler.TransferResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != transferID {
		t.Fatalf("transfer ID mismatch: got %s, want %s", resp.ID, transferID)
	}

	if resp.Amount < 100 || resp.Amount > 10_000 {
		t.Fatalf("amount %d outside expected range [100, 10000]", resp.Amount)
	}
}

func TestHandler_MagicDeposit_200_Idempotent(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{
		magicResp: service.TransferResponse{
			TransferID: domain.NewID(),
			Status:     domain.TransferStatusProcessed,
			Amount:     1_000,
		},
	}
	router := newRouter(svc)

	body := handler.MagicDepositRequest{
		IdempotencyKey: "magic-idem-001",
		ToWalletID:     domain.NewID(),
	}

	req1 := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader(toJSON(t, body)))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader(toJSON(t, body)))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d", rec2.Code)
	}
}

func TestHandler_MagicDeposit_400_MissingIdempotencyKey(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}
	body := handler.MagicDepositRequest{
		ToWalletID: domain.NewID(),
		// IdempotencyKey intentionally omitted
	}

	req := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_MagicDeposit_400_IdempotencyKeyTooLong(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}

	key := make([]byte, service.MaxIdempotencyKeyLength+1)
	for i := range key {
		key[i] = 'a'
	}

	body := handler.MagicDepositRequest{
		IdempotencyKey: string(key),
		ToWalletID:     domain.NewID(),
	}

	req := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong idempotency key, got %d", rec.Code)
	}
}

func TestHandler_MagicDeposit_400_MissingToWalletID(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}
	body := handler.MagicDepositRequest{
		IdempotencyKey: "magic-key-001",
		// ToWalletID intentionally zero-value
	}

	req := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing toWalletId, got %d", rec.Code)
	}
}

func TestHandler_MagicDeposit_404_WalletNotFound(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{magicErr: domain.ErrWalletNotFound}
	body := handler.MagicDepositRequest{
		IdempotencyKey: "magic-key-001",
		ToWalletID:     domain.NewID(),
	}

	req := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_MagicDeposit_400_BadJSON(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}

	req := httptest.NewRequest(http.MethodPost, "/magic", bytes.NewReader([]byte(`{bad json}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}
