package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"shindechandrakant/internal/api/controllers"
	"shindechandrakant/internal/api/dtos"
	"shindechandrakant/internal/wallet"
)

// ---------------------------------------------------------------------------
// Mock service
// ---------------------------------------------------------------------------

type mockService struct {
	transferFn func(ctx context.Context, req dtos.TransferRequest) (*wallet.Transfer, error)
}

func (m *mockService) Transfer(ctx context.Context, req dtos.TransferRequest) (*wallet.Transfer, error) {
	return m.transferFn(ctx, req)
}

// ---------------------------------------------------------------------------
// Test helper
// ---------------------------------------------------------------------------

func newTestApp(svc wallet.Service) *fiber.App {
	app := fiber.New()
	ctrl := controllers.NewWalletController(svc)
	app.Post("/transfers", ctrl.Transfer)
	return app
}

func doRequest(app *fiber.App, body any) *http.Response {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	return resp
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestTransferHandler_Success(t *testing.T) {
	svc := &mockService{
		transferFn: func(_ context.Context, _ dtos.TransferRequest) (*wallet.Transfer, error) {
			return &wallet.Transfer{
				TransactionId:  "tx-001",
				IdempotencyKey: "key-001",
				FromWalletId:   "wallet_1",
				ToWalletId:     "wallet_2",
				Amount:         2000,
				State:          wallet.StateProcessed,
				CreatedAt:      time.Now(),
			}, nil
		},
	}

	resp := doRequest(newTestApp(svc), map[string]any{
		"idempotencyKey": "key-001",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         20.00,
	})

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	var body dtos.TransferResponse
	json.NewDecoder(resp.Body).Decode(&body)

	if body.TransactionId != "tx-001" {
		t.Errorf("expected transactionId tx-001, got %s", body.TransactionId)
	}
	if body.Amount != 20.00 {
		t.Errorf("expected amount 20.00, got %f", body.Amount)
	}
	if body.Status != "PROCESSED" {
		t.Errorf("expected status PROCESSED, got %s", body.Status)
	}
}

func TestTransferHandler_InvalidBody(t *testing.T) {
	app := newTestApp(&mockService{})
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestTransferHandler_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing idempotencyKey", map[string]any{"fromWalletId": "w1", "toWalletId": "w2", "amount": 10}},
		{"idempotencyKey too short", map[string]any{"idempotencyKey": "ab", "fromWalletId": "w1", "toWalletId": "w2", "amount": 10}},
		{"missing fromWalletId", map[string]any{"idempotencyKey": "key-001", "toWalletId": "w2", "amount": 10}},
		{"missing toWalletId", map[string]any{"idempotencyKey": "key-001", "fromWalletId": "w1", "amount": 10}},
		{"amount zero", map[string]any{"idempotencyKey": "key-001", "fromWalletId": "w1", "toWalletId": "w2", "amount": 0}},
		{"amount negative", map[string]any{"idempotencyKey": "key-001", "fromWalletId": "w1", "toWalletId": "w2", "amount": -5}},
	}

	app := newTestApp(&mockService{})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(app, tc.body)
			if resp.StatusCode != fiber.StatusUnprocessableEntity {
				t.Errorf("%s: expected 422, got %d", tc.name, resp.StatusCode)
			}
		})
	}
}

func TestTransferHandler_InsufficientFunds(t *testing.T) {
	svc := &mockService{
		transferFn: func(_ context.Context, _ dtos.TransferRequest) (*wallet.Transfer, error) {
			return nil, wallet.ErrInsufficientFunds
		},
	}

	resp := doRequest(newTestApp(svc), map[string]any{
		"idempotencyKey": "key-002",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         9999.00,
	})

	if resp.StatusCode != fiber.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestTransferHandler_WalletNotFound(t *testing.T) {
	svc := &mockService{
		transferFn: func(_ context.Context, _ dtos.TransferRequest) (*wallet.Transfer, error) {
			return nil, wallet.ErrWalletNotFound
		},
	}

	resp := doRequest(newTestApp(svc), map[string]any{
		"idempotencyKey": "key-003",
		"fromWalletId":   "wallet_x",
		"toWalletId":     "wallet_2",
		"amount":         10.00,
	})

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTransferHandler_SameWallet(t *testing.T) {
	svc := &mockService{
		transferFn: func(_ context.Context, _ dtos.TransferRequest) (*wallet.Transfer, error) {
			return nil, wallet.ErrSameWallet
		},
	}

	resp := doRequest(newTestApp(svc), map[string]any{
		"idempotencyKey": "key-004",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_1",
		"amount":         10.00,
	})

	if resp.StatusCode != fiber.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestTransferHandler_InternalError(t *testing.T) {
	svc := &mockService{
		transferFn: func(_ context.Context, _ dtos.TransferRequest) (*wallet.Transfer, error) {
			return nil, context.DeadlineExceeded
		},
	}

	resp := doRequest(newTestApp(svc), map[string]any{
		"idempotencyKey": "key-005",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         10.00,
	})

	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}
