package route_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/handler"
	"wallet-transfer-assignment/internal/feature/wallet/route"
	"wallet-transfer-assignment/internal/feature/wallet/service"

	"github.com/gofiber/fiber/v3"
)

type stubService struct{ calls int }

func (s *stubService) CreateTransfer(context.Context, service.CreateTransferRequest) (*service.TransferResult, error) {
	s.calls++
	return &service.TransferResult{StatusCode: http.StatusCreated, Body: []byte(`{"ok":true}`)}, nil
}

func newApp(svc service.TransferService) *fiber.App {
	app := fiber.New()
	route.RegisterRoutes(app, route.Handlers{Transfer: handler.NewTransferHandler(svc)})
	return app
}

func do(t *testing.T, app *fiber.App, method, path, body string) (status int, respBody string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPostTransfersIsRoutedToTheHandler(t *testing.T) {
	svc := &stubService{}
	status, body := do(t, newApp(svc), http.MethodPost, "/transfers",
		`{"idempotencyKey":"key-12345","fromWalletId":1,"toWalletId":2,"amount":10}`)

	if status != http.StatusCreated || body != `{"ok":true}` || svc.calls != 1 {
		t.Fatalf("status=%d body=%s calls=%d", status, body, svc.calls)
	}
}

func TestOnlyPostTransfersIsExposed(t *testing.T) {
	tests := []struct{ method, path string }{
		{http.MethodGet, "/transfers"},
		{http.MethodPut, "/transfers"},
		{http.MethodDelete, "/transfers"},
		{http.MethodPost, "/transfers/1"},
		{http.MethodPost, "/wallets"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			svc := &stubService{}
			status, _ := do(t, newApp(svc), tc.method, tc.path, "{}")

			if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 404 or 405", status)
			}
			if svc.calls != 0 {
				t.Fatal("service must not be called")
			}
		})
	}
}
