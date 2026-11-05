package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type walletServiceStub struct {
	wallet *domain.Wallet
	err    error
}

func (s *walletServiceStub) CreateWallet(_ context.Context, input service.CreateWalletInput) (*domain.Wallet, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &domain.Wallet{ID: input.ID, Balance: input.Balance, Status: domain.WalletStatusActive, CreatedAt: "now", UpdatedAt: "now"}, nil
}
func (s *walletServiceStub) GetWallet(context.Context, string) (*domain.Wallet, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.wallet, nil
}
func (s *walletServiceStub) AddFunds(_ context.Context, walletID string, amount int64) (*domain.Wallet, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &domain.Wallet{ID: walletID, Balance: amount, Status: domain.WalletStatusActive}, nil
}
func (s *walletServiceStub) GetLedger(context.Context, string) ([]domain.LedgerEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []domain.LedgerEntry{{ID: "entry-1", WalletID: "wallet-1", Type: domain.LedgerEntryCredit, Amount: 500}}, nil
}

func TestWalletHandlerRoutes(t *testing.T) {
	stub := &walletServiceStub{wallet: &domain.Wallet{
		ID: "wallet-1", Balance: 1000, Status: domain.WalletStatusActive,
		CreatedAt: "created", UpdatedAt: "updated",
	}}
	mux := http.NewServeMux()
	NewWalletHandler(stub).RegisterRoutes(mux)

	cases := []struct {
		name, method, path, body string
		wantStatus               int
	}{
		{"create", http.MethodPost, "/wallets", `{"walletId":"wallet-1","initialBalance":1000}`, http.StatusCreated},
		{"get", http.MethodGet, "/wallets/wallet-1", "", http.StatusOK},
		{"fund", http.MethodPost, "/wallets/wallet-1/funds", `{"amount":500}`, http.StatusOK},
		{"ledger", http.MethodGet, "/wallets/wallet-1/ledger", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("content type = %q, want application/json", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestWalletHandlerMapsValidationAndNotFoundErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"invalid", domain.ErrInvalidWallet, http.StatusBadRequest},
		{"not found", domain.ErrWalletNotFound, http.StatusNotFound},
		{"inactive", domain.ErrWalletInactive, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			NewWalletHandler(&walletServiceStub{err: tc.err}).RegisterRoutes(mux)
			req := httptest.NewRequest(http.MethodPost, "/wallets/wallet-1/funds", strings.NewReader(`{"amount":10}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var response map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response["error"] == "" {
				t.Fatalf("missing error response: %v", response)
			}
		})
	}
}

func TestWalletHandlerRejectsMalformedAndUnknownFields(t *testing.T) {
	mux := http.NewServeMux()
	NewWalletHandler(&walletServiceStub{}).RegisterRoutes(mux)
	for _, body := range []string{`{"amount":`, `{"amount":10,"idempotencyKey":"not-needed"}`} {
		req := httptest.NewRequest(http.MethodPost, "/wallets/wallet-1/funds", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
}
