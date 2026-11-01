package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestCreateTransferRejectsFractionalAmount(t *testing.T) {
	api := &stubAPI{}
	srv := httptest.NewServer(New(api))
	t.Cleanup(srv.Close)

	res := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100.5}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", res.StatusCode)
	}
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "must be integers") {
		t.Fatalf("body = %s", payload)
	}
	if api.transferCalls != 0 {
		t.Fatal("invalid JSON reached the service")
	}
}

func TestCreateTransferRejectsUnknownField(t *testing.T) {
	api := &stubAPI{}
	srv := httptest.NewServer(New(api))
	t.Cleanup(srv.Close)

	res := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100,"note":"x"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if api.transferCalls != 0 {
		t.Fatal("unknown field reached the service")
	}
}

func TestCreateRoutesRejectTrailingData(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "transfer followed by JSON",
			path: "/transfers",
			body: `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100} {}`,
		},
		{
			name: "wallet followed by oversized whitespace",
			path: "/wallets",
			body: `{"id":"wallet_1","initialBalance":100}` + strings.Repeat(" ", maxBodyBytes),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &stubAPI{}
			srv := httptest.NewServer(New(api))
			t.Cleanup(srv.Close)

			res := postJSON(t, srv.URL+tc.path, tc.body)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d", res.StatusCode)
			}
			if api.transferCalls != 0 || api.createWalletCalls != 0 {
				t.Fatal("trailing data reached the service")
			}
		})
	}
}

func TestCreateTransferReplayHeader(t *testing.T) {
	body := []byte(`{"transferId":"t1","status":"PROCESSED"}`)
	api := &stubAPI{
		transfer: func(service.Request) (service.Result, error) {
			return service.Result{Status: http.StatusCreated, Body: body, Replayed: true, TransferID: "t1"}, nil
		},
	}
	srv := httptest.NewServer(New(api))
	t.Cleanup(srv.Close)

	res := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if res.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatal("missing replay header")
	}
	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("body = %s", got)
	}
}

func TestCreateTransferMapsNotFoundAndConflict(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "missing wallet", err: domain.ErrNotFound, status: http.StatusNotFound},
		{name: "payload mismatch", err: domain.ErrIdempotencyConflict, status: http.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &stubAPI{
				transfer: func(service.Request) (service.Result, error) {
					return service.Result{}, tc.err
				},
			}
			srv := httptest.NewServer(New(api))
			t.Cleanup(srv.Close)
			res := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
			if res.StatusCode != tc.status {
				t.Fatalf("status = %d", res.StatusCode)
			}
		})
	}
}

func TestCreateWalletConflict(t *testing.T) {
	api := &stubAPI{
		createWallet: func(string, int64) (service.WalletView, error) {
			return service.WalletView{}, domain.ErrAlreadyExists
		},
	}
	srv := httptest.NewServer(New(api))
	t.Cleanup(srv.Close)
	res := postJSON(t, srv.URL+"/wallets", `{"id":"wallet_1","initialBalance":10}`)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestGetWallet(t *testing.T) {
	api := &stubAPI{
		getWallet: func(id string) (service.WalletView, error) {
			if id != "wallet_1" {
				t.Fatalf("id = %s", id)
			}
			return service.WalletView{ID: id, Balance: 40, InitialBalance: 50}, nil
		},
	}
	srv := httptest.NewServer(New(api))
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/wallets/wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestRequestDeadlineIsShorterThanWriteTimeout(t *testing.T) {
	api := &deadlineAPI{}
	handler := WithTimeout(New(api), 8*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !api.hasDeadline {
		t.Fatal("request context has no deadline")
	}
	remaining := time.Until(api.deadline)
	if remaining <= 0 || remaining >= 10*time.Second {
		t.Fatalf("deadline remaining = %s", remaining)
	}
}

func TestDeadlineExceededReturnsGatewayTimeout(t *testing.T) {
	api := &stubAPI{
		transfer: func(service.Request) (service.Result, error) {
			return service.Result{}, context.DeadlineExceeded
		},
	}
	srv := httptest.NewServer(New(api))
	t.Cleanup(srv.Close)
	res := postJSON(t, srv.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if res.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

type deadlineAPI struct {
	stubAPI
	deadline    time.Time
	hasDeadline bool
}

func (d *deadlineAPI) GetWallet(ctx context.Context, id string) (service.WalletView, error) {
	d.deadline, d.hasDeadline = ctx.Deadline()
	return service.WalletView{ID: id, Balance: 1, InitialBalance: 1}, nil
}

type stubAPI struct {
	transfer          func(service.Request) (service.Result, error)
	createWallet      func(string, int64) (service.WalletView, error)
	getWallet         func(string) (service.WalletView, error)
	getTransfer       func(string) (service.TransferDetails, error)
	transferCalls     int
	createWalletCalls int
}

func (s *stubAPI) CreateTransfer(_ context.Context, req service.Request) (service.Result, error) {
	s.transferCalls++
	if s.transfer == nil {
		return service.Result{}, domain.ErrNotFound
	}
	return s.transfer(req)
}

func (s *stubAPI) CreateWallet(_ context.Context, id string, initial int64) (service.WalletView, error) {
	s.createWalletCalls++
	if s.createWallet == nil {
		return service.WalletView{}, domain.ErrNotFound
	}
	return s.createWallet(id, initial)
}

func (s *stubAPI) GetWallet(_ context.Context, id string) (service.WalletView, error) {
	if s.getWallet == nil {
		return service.WalletView{}, domain.ErrNotFound
	}
	return s.getWallet(id)
}

func (s *stubAPI) GetTransfer(_ context.Context, id string) (service.TransferDetails, error) {
	if s.getTransfer == nil {
		return service.TransferDetails{}, domain.ErrNotFound
	}
	return s.getTransfer(id)
}
