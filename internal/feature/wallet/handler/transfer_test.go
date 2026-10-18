package handler_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/handler"
	"wallet-transfer-assignment/internal/feature/wallet/repository"
	"wallet-transfer-assignment/internal/feature/wallet/route"
	"wallet-transfer-assignment/internal/feature/wallet/service"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type stubService struct {
	calls  int
	gotReq service.CreateTransferRequest
	result *service.TransferResult
	err    error
}

func (s *stubService) CreateTransfer(_ context.Context, req service.CreateTransferRequest) (*service.TransferResult, error) {
	s.calls++
	s.gotReq = req
	return s.result, s.err
}

func newApp(svc service.TransferService) *fiber.App {
	app := fiber.New()
	route.RegisterRoutes(app, route.Handlers{Transfer: handler.NewTransferHandler(svc)})
	return app
}

func post(t *testing.T, app *fiber.App, body string) (status int, respBody string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func body(key string, from, to, amount int64) string {
	return fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%d,"toWalletId":%d,"amount":%d}`, key, from, to, amount)
}

func TestCreateTransfer_PassesRequestToServiceAndReturnsItsResponseVerbatim(t *testing.T) {
	svc := &stubService{result: &service.TransferResult{StatusCode: 201, Body: []byte(`{"transferId":9,"status":"PROCESSED"}`)}}

	status, got := post(t, newApp(svc), body("key-12345", 1, 2, 100))

	if status != 201 || got != `{"transferId":9,"status":"PROCESSED"}` {
		t.Fatalf("got %d %s", status, got)
	}
	want := service.CreateTransferRequest{IdempotencyKey: "key-12345", FromWalletID: 1, ToWalletID: 2, Amount: 100}
	if svc.gotReq != want {
		t.Fatalf("service received %+v, want %+v", svc.gotReq, want)
	}
}

// A replayed 409 (insufficient funds) must be returned with the stored status, not turned into 200/201.
func TestCreateTransfer_ReplaysStoredNonSuccessStatus(t *testing.T) {
	svc := &stubService{result: &service.TransferResult{StatusCode: 409, Body: []byte(`{"error":"insufficient funds"}`)}}

	status, got := post(t, newApp(svc), body("key-12345", 1, 2, 100))

	if status != 409 || got != `{"error":"insufficient funds"}` {
		t.Fatalf("got %d %s", status, got)
	}
}

func TestCreateTransfer_RejectsInvalidInputWithoutCallingService(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"idempotencyKey":`},
		{"empty body", ``},
		{"missing key", body("", 1, 2, 100)},
		{"blank key", body("   ", 1, 2, 100)},
		{"key under 8 chars", body("short12", 1, 2, 100)},
		{"key of 8 spaces", body("        ", 1, 2, 100)},
		{"key over 64 chars", body(strings.Repeat("k", 65), 1, 2, 100)},
		{"zero sender", body("key-12345", 0, 2, 100)},
		{"negative receiver", body("key-12345", 1, -2, 100)},
		{"same wallets", body("key-12345", 3, 3, 100)},
		{"zero amount", body("key-12345", 1, 2, 0)},
		{"negative amount", body("key-12345", 1, 2, -100)},
		{"string wallet id", `{"idempotencyKey":"key-12345","fromWalletId":"wallet_1","toWalletId":2,"amount":1}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubService{}
			status, got := post(t, newApp(svc), tc.body)

			if status != 400 {
				t.Fatalf("status = %d (%s), want 400", status, got)
			}
			if svc.calls != 0 {
				t.Fatalf("service called %d times for invalid input", svc.calls)
			}
		})
	}
}

func TestCreateTransfer_MapsServiceErrorsToStatusCodes(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"invalid request", fmt.Errorf("%w: x", service.ErrInvalidTransferRequest), 400},
		{"idempotency conflict", domain.ErrIdempotencyConflict, 409},
		{"request in progress", service.ErrRequestInProgress, 409},
		{"wallet not found", fmt.Errorf("lock first wallet: %w", domain.ErrWalletNotFound), 404},
		{"gorm record not found", gorm.ErrRecordNotFound, 404},
		{"commit uncertain", fmt.Errorf("%w: conn reset", repository.ErrCommitUncertain), 503},
		{"unknown error", errors.New("boom"), 500},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubService{err: tc.err}
			status, got := post(t, newApp(svc), body("key-12345", 1, 2, 100))

			if status != tc.status {
				t.Fatalf("status = %d, want %d", status, tc.status)
			}
			if !strings.Contains(got, `"error"`) {
				t.Fatalf("error response should carry an error field, got %s", got)
			}
			// Internal error text must not leak to clients.
			if tc.status == 500 && strings.Contains(got, "boom") {
				t.Fatalf("internal error leaked to client: %s", got)
			}
		})
	}
}
