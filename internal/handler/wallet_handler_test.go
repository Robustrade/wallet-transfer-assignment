package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallettransfer/internal/domain"
)

type fakeWalletStore struct {
	wallet domain.Wallet
	err    error
}

func (f *fakeWalletStore) Create(ctx context.Context, walletID string, openingBalance int64) (domain.Wallet, error) {
	if f.err != nil {
		return domain.Wallet{}, f.err
	}
	return domain.Wallet{ID: walletID, Balance: openingBalance}, nil
}

func (f *fakeWalletStore) Get(ctx context.Context, walletID string) (domain.Wallet, error) {
	if f.err != nil {
		return domain.Wallet{}, f.err
	}
	return f.wallet, nil
}

func doCreateWallet(t *testing.T, h *WalletHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.CreateWallet(rec, req)
	return rec
}

func doGetWallet(t *testing.T, h *WalletHandler, id string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /wallets/{id}", h.GetWallet)
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+id, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCreateWallet_Success_Returns201(t *testing.T) {
	store := &fakeWalletStore{}
	h := NewWalletHandler(store, store)

	rec := doCreateWallet(t, h, `{"id":"alice","openingBalance":500}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp walletResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != "alice" || resp.Balance != 500 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestCreateWallet_InvalidJSON_Returns400(t *testing.T) {
	store := &fakeWalletStore{}
	h := NewWalletHandler(store, store)
	rec := doCreateWallet(t, h, `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateWallet_EmptyID_Returns400(t *testing.T) {
	store := &fakeWalletStore{}
	h := NewWalletHandler(store, store)
	rec := doCreateWallet(t, h, `{"id":"","openingBalance":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateWallet_NegativeOpeningBalance_Returns400(t *testing.T) {
	store := &fakeWalletStore{}
	h := NewWalletHandler(store, store)
	rec := doCreateWallet(t, h, `{"id":"alice","openingBalance":-1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateWallet_StoreError_Returns500(t *testing.T) {
	store := &fakeWalletStore{err: errors.New("db down")}
	h := NewWalletHandler(store, store)
	rec := doCreateWallet(t, h, `{"id":"alice","openingBalance":0}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestGetWallet_Success_Returns200(t *testing.T) {
	store := &fakeWalletStore{wallet: domain.Wallet{ID: "alice", Balance: 250}}
	h := NewWalletHandler(store, store)

	rec := doGetWallet(t, h, "alice")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp walletResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != "alice" || resp.Balance != 250 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGetWallet_NotFound_Returns404(t *testing.T) {
	store := &fakeWalletStore{err: domain.ErrWalletNotFound}
	h := NewWalletHandler(store, store)
	rec := doGetWallet(t, h, "missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetWallet_StoreError_Returns500(t *testing.T) {
	store := &fakeWalletStore{err: errors.New("db down")}
	h := NewWalletHandler(store, store)
	rec := doGetWallet(t, h, "alice")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
