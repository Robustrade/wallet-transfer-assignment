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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Helper ────────────────────────────────────────────────────────────────────

func postTransfer(t *testing.T, h *handler.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.CreateTransfer(rr, req)
	return rr
}

// ── CreateTransfer ────────────────────────────────────────────────────────────

func TestCreateTransfer_Success(t *testing.T) {
	expected := &domain.Transfer{
		ID:     "t-1",
		Amount: 100,
		Status: domain.StatusProcessed,
	}

	h := handler.New(&mockService{
		createTransferFn: func(_ context.Context, _ service.CreateTransferRequest) (*domain.Transfer, error) {
			return expected, nil
		},
	})

	rr := postTransfer(t, h, `{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":100}`)

	assert.Equal(t, http.StatusCreated, rr.Code)
	var got domain.Transfer
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, "t-1", got.ID)
	assert.Equal(t, domain.StatusProcessed, got.Status)
}

func TestCreateTransfer_MissingIdempotencyKey(t *testing.T) {
	h := handler.New(&mockService{})

	rr := postTransfer(t, h, `{"fromWalletId":"w1","toWalletId":"w2","amount":100}`)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateTransfer_MissingWalletID(t *testing.T) {
	h := handler.New(&mockService{})

	rr := postTransfer(t, h, `{"idempotencyKey":"k1","toWalletId":"w2","amount":100}`)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateTransfer_ZeroAmount(t *testing.T) {
	h := handler.New(&mockService{})

	rr := postTransfer(t, h, `{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":0}`)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateTransfer_InvalidJSON(t *testing.T) {
	h := handler.New(&mockService{})

	rr := postTransfer(t, h, `{not valid json}`)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	failed := &domain.Transfer{
		ID:     "t-2",
		Status: domain.StatusFailed,
	}

	h := handler.New(&mockService{
		createTransferFn: func(_ context.Context, _ service.CreateTransferRequest) (*domain.Transfer, error) {
			return failed, domain.ErrInsufficientFunds
		},
	})

	rr := postTransfer(t, h, `{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":9999}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	var got domain.Transfer
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, domain.StatusFailed, got.Status)
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	h := handler.New(&mockService{
		createTransferFn: func(_ context.Context, _ service.CreateTransferRequest) (*domain.Transfer, error) {
			return nil, domain.ErrWalletNotFound
		},
	})

	rr := postTransfer(t, h, `{"idempotencyKey":"k1","fromWalletId":"ghost","toWalletId":"w2","amount":50}`)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
