package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wallet-transfer-assignment/internal/database"
	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/repository"
	"github.com/wallet-transfer-assignment/internal/service"
)

// testHandler creates a real handler backed by a SQLite in-memory database.
func testHandler(t *testing.T) *TransferHandler {
	t.Helper()

	db, err := database.NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	if err := database.RunSQLiteMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Seed wallets
	for _, w := range []struct {
		id      string
		balance int64
	}{
		{"wallet_1", 10000},
		{"wallet_2", 10000},
	} {
		_, err := db.Exec("INSERT INTO wallets (id, balance) VALUES (?, ?)", w.id, w.balance)
		if err != nil {
			t.Fatalf("failed to seed wallet: %v", err)
		}
	}

	repo := repository.NewSQLiteRepository(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc := service.NewTransferService(repo, logger)
	h := NewTransferHandler(svc, logger)

	t.Cleanup(func() { db.Close() })

	return h
}

func TestHTTPBadRequest(t *testing.T) {
	h := testHandler(t)

	tests := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{bad json`},
		{"empty body", ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleCreateTransfer(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestHTTPSuccessfulTransfer(t *testing.T) {
	h := testHandler(t)

	body, _ := json.Marshal(domain.TransferRequest{
		IdempotencyKey: "http-test-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         500,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleCreateTransfer(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp domain.TransferResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Status != domain.TransferStatusProcessed {
		t.Errorf("expected PROCESSED, got %s", resp.Status)
	}
	if resp.Amount != 500 {
		t.Errorf("expected amount 500, got %d", resp.Amount)
	}
}

func TestHTTPIdempotentRetry(t *testing.T) {
	h := testHandler(t)

	body, _ := json.Marshal(domain.TransferRequest{
		IdempotencyKey: "http-idempotent-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	h.HandleCreateTransfer(w1, req1)

	// Second request (duplicate)
	req2 := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.HandleCreateTransfer(w2, req2)

	// Both should return 200
	if w1.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", w1.Code)
	}
	if w2.Code != http.StatusOK {
		t.Errorf("second request: expected 200, got %d", w2.Code)
	}

	// Both should return identical responses
	if w1.Body.String() != w2.Body.String() {
		t.Errorf("responses differ:\n  first:  %s\n  second: %s", w1.Body.String(), w2.Body.String())
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	h := testHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	w := httptest.NewRecorder()

	h.HandleCreateTransfer(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHTTPValidationErrors(t *testing.T) {
	h := testHandler(t)

	tests := []struct {
		name       string
		req        domain.TransferRequest
		wantStatus int
	}{
		{
			name: "same wallet",
			req: domain.TransferRequest{
				IdempotencyKey: "val-1",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_1",
				Amount:         100,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "zero amount",
			req: domain.TransferRequest{
				IdempotencyKey: "val-2",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         0,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing idempotency key",
			req: domain.TransferRequest{
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.req)
			req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleCreateTransfer(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestHTTPInsufficientBalance(t *testing.T) {
	h := testHandler(t)

	body, _ := json.Marshal(domain.TransferRequest{
		IdempotencyKey: "http-insufficient-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         50000,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleCreateTransfer(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}

	var resp domain.TransferResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != domain.TransferStatusFailed {
		t.Errorf("expected FAILED, got %s", resp.Status)
	}
}

func TestHTTPWalletNotFound(t *testing.T) {
	h := testHandler(t)

	body, _ := json.Marshal(domain.TransferRequest{
		IdempotencyKey: "not-found-http",
		FromWalletID:   "nonexistent",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleCreateTransfer(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
