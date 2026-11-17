package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/dto"
	"github.com/Robustrade/wallet-transfer-assignment/handler"
	"github.com/Robustrade/wallet-transfer-assignment/service"
)

func TestCreateWallet_Validation(t *testing.T) {
	t.Parallel()
	h := handler.New(nil)
	mux := http.NewServeMux()
	h.Routes(mux)

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{`},
		{"empty id", `{"id":"","balance":10}`},
		{"whitespace id", `{"id":"   ","balance":10}`},
		{"missing balance", `{"id":"w1"}`},
		{"negative balance", `{"id":"w1","balance":-1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"invalid_request"`) {
				t.Fatalf("body=%s", rec.Body.String())
			}
		})
	}
}

func TestCreateWallet_Success(t *testing.T) {
	t.Parallel()
	want := dto.WalletView{ID: "w1", OpeningBalance: 100, Balance: 100}
	h := handler.New(&stubService{createWalletView: want})
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"id":"w1","balance":100}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.WalletView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestCreateWallet_AlreadyExists(t *testing.T) {
	t.Parallel()
	h := handler.New(&stubService{createWalletErr: service.ErrWalletAlreadyExists})
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"id":"w1","balance":10}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"wallet_already_exists"`) {
		t.Fatalf("body=%s", body)
	}
	if !strings.Contains(body, "choose a different one") {
		t.Fatalf("expected message in body=%s", body)
	}
}

func TestCreateWallet_InternalServerError(t *testing.T) {
	t.Parallel()
	h := handler.New(&stubService{createWalletErr: errors.New("db down")})
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"id":"w1","balance":10}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"internal"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestGetWallet_Success(t *testing.T) {
	t.Parallel()
	want := dto.WalletView{ID: "w1", OpeningBalance: 50, Balance: 40}
	h := handler.New(&stubService{getWalletView: want})
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodGet, "/wallets/w1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.WalletView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestGetWallet_NotFound(t *testing.T) {
	t.Parallel()
	h := handler.New(&stubService{getWalletErr: service.ErrWalletNotFound})
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodGet, "/wallets/missing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"wallet_not_found"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestGetWallet_InternalServerError(t *testing.T) {
	t.Parallel()
	h := handler.New(&stubService{getWalletErr: errors.New("db down")})
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodGet, "/wallets/w1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"internal"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
