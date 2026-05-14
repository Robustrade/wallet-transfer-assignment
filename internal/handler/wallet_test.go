package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func postWallet(t *testing.T, h *handler.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateWallet(rr, req)
	return rr
}

func getWallet(t *testing.T, h *handler.Handler, walletID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/wallets/{walletId}", h.GetWallet)
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+walletID, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func getTransferHistory(t *testing.T, h *handler.Handler, walletID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/wallets/{walletId}/transfers", h.GetTransferHistory)
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+walletID+"/transfers", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// ── CreateWallet ──────────────────────────────────────────────────────────────

func TestCreateWallet_Success(t *testing.T) {
	h := handler.New(&mockService{
		createWalletFn: func(_ context.Context, req service.CreateWalletRequest) (*domain.Wallet, error) {
			return &domain.Wallet{ID: "w-1", Balance: req.InitialBalance}, nil
		},
	})

	rr := postWallet(t, h, `{"initialBalance":500}`)

	assert.Equal(t, http.StatusCreated, rr.Code)
	var got domain.Wallet
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, "w-1", got.ID)
	assert.Equal(t, int64(500), got.Balance)
}

func TestCreateWallet_InvalidBody(t *testing.T) {
	h := handler.New(&mockService{})

	rr := postWallet(t, h, `{not valid json}`)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateWallet_NegativeBalance(t *testing.T) {
	h := handler.New(&mockService{
		createWalletFn: func(_ context.Context, _ service.CreateWalletRequest) (*domain.Wallet, error) {
			return nil, domain.ErrInvalidAmount
		},
	})

	rr := postWallet(t, h, `{"initialBalance":-100}`)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateWallet_InternalError(t *testing.T) {
	h := handler.New(&mockService{
		createWalletFn: func(_ context.Context, _ service.CreateWalletRequest) (*domain.Wallet, error) {
			return nil, assert.AnError
		},
	})

	rr := postWallet(t, h, `{"initialBalance":0}`)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ── GetWallet ─────────────────────────────────────────────────────────────────

func TestGetWallet_Success(t *testing.T) {
	h := handler.New(&mockService{
		getWalletFn: func(_ context.Context, id string) (*domain.Wallet, error) {
			return &domain.Wallet{ID: id, Balance: 500}, nil
		},
	})

	rr := getWallet(t, h, "w-1")

	assert.Equal(t, http.StatusOK, rr.Code)
	var got domain.Wallet
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, "w-1", got.ID)
	assert.Equal(t, int64(500), got.Balance)
}

func TestGetWallet_NotFound(t *testing.T) {
	h := handler.New(&mockService{
		getWalletFn: func(_ context.Context, _ string) (*domain.Wallet, error) {
			return nil, domain.ErrWalletNotFound
		},
	})

	rr := getWallet(t, h, "missing")

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// ── GetTransferHistory ────────────────────────────────────────────────────────

func TestGetTransferHistory_Success(t *testing.T) {
	transfers := []domain.Transfer{
		{ID: "t-1", Amount: 100, Status: domain.StatusProcessed},
		{ID: "t-2", Amount: 200, Status: domain.StatusProcessed},
	}

	h := handler.New(&mockService{
		getTransferHistFn: func(_ context.Context, _ string) ([]domain.Transfer, error) {
			return transfers, nil
		},
	})

	rr := getTransferHistory(t, h, "w-1")

	assert.Equal(t, http.StatusOK, rr.Code)
	var got []domain.Transfer
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Len(t, got, 2)
	assert.Equal(t, "t-1", got[0].ID)
}

func TestGetTransferHistory_Empty(t *testing.T) {
	h := handler.New(&mockService{
		getTransferHistFn: func(_ context.Context, _ string) ([]domain.Transfer, error) {
			return nil, nil
		},
	})

	rr := getTransferHistory(t, h, "w-1")

	assert.Equal(t, http.StatusOK, rr.Code)
	var got []domain.Transfer
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.NotNil(t, got)  // must be [] not null
	assert.Len(t, got, 0)
}

func TestGetTransferHistory_WalletNotFound(t *testing.T) {
	h := handler.New(&mockService{
		getTransferHistFn: func(_ context.Context, _ string) ([]domain.Transfer, error) {
			return nil, domain.ErrWalletNotFound
		},
	})

	rr := getTransferHistory(t, h, "ghost")

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
