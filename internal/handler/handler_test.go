package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
	"github.com/ferozyk/wallet-transfer-assignment/internal/service"
)

// fakeService is a configurable test double implementing service.TransferService.
type fakeService struct {
	transferResult *service.TransferResult
	transferErr    error

	getTransfer    *domain.Transfer
	getTransferErr error

	balance    *service.WalletBalance
	balanceErr error

	lastInput service.TransferInput
}

func (f *fakeService) Transfer(_ context.Context, in service.TransferInput) (*service.TransferResult, error) {
	f.lastInput = in
	return f.transferResult, f.transferErr
}

func (f *fakeService) GetTransfer(_ context.Context, _ string) (*domain.Transfer, error) {
	return f.getTransfer, f.getTransferErr
}

func (f *fakeService) GetWalletBalance(_ context.Context, _ string) (*service.WalletBalance, error) {
	return f.balance, f.balanceErr
}

func sampleTransfer(status domain.TransferStatus) *domain.Transfer {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	return &domain.Transfer{
		ID:             "t-123",
		IdempotencyKey: "key-1",
		FromWalletID:   "w-from",
		ToWalletID:     "w-to",
		Amount:         500,
		Status:         status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func doRequest(t *testing.T, h http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		switch b := body.(type) {
		case string:
			buf.WriteString(b)
		default:
			require.NoError(t, json.NewEncoder(&buf).Encode(b))
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPostTransferSuccess(t *testing.T) {
	svc := &fakeService{transferResult: &service.TransferResult{
		Transfer: sampleTransfer(domain.StatusProcessed),
		Replayed: false,
	}}
	h := New(svc)

	rec := doRequest(t, h, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "w-from",
		ToWalletID:     "w-to",
		Amount:         500,
	})

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Empty(t, rec.Header().Get("Idempotent-Replayed"))

	var resp transferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "t-123", resp.Id)
	assert.Equal(t, string(domain.StatusProcessed), resp.Status)
	assert.Equal(t, int64(500), resp.Amount)
	assert.Equal(t, "w-from", resp.FromWalletId)
	assert.Equal(t, "w-to", resp.ToWalletId)

	// Request was mapped onto the service input.
	assert.Equal(t, "key-1", svc.lastInput.IdempotencyKey)
	assert.Equal(t, int64(500), int64(svc.lastInput.Amount))
}

func TestPostTransferReplay(t *testing.T) {
	transfer := sampleTransfer(domain.StatusProcessed)

	// First (fresh) response.
	fresh := New(&fakeService{transferResult: &service.TransferResult{Transfer: transfer, Replayed: false}})
	firstRec := doRequest(t, fresh, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w-from", ToWalletID: "w-to", Amount: 500,
	})
	require.Equal(t, http.StatusCreated, firstRec.Code)

	// Replayed response for the same transfer.
	replay := New(&fakeService{transferResult: &service.TransferResult{Transfer: transfer, Replayed: true}})
	replayRec := doRequest(t, replay, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w-from", ToWalletID: "w-to", Amount: 500,
	})

	require.Equal(t, http.StatusOK, replayRec.Code)
	assert.Equal(t, "true", replayRec.Header().Get("Idempotent-Replayed"))

	// Exactly-once surface: identical body bytes between fresh and replay.
	assert.Equal(t, firstRec.Body.Bytes(), replayRec.Body.Bytes())
}

func TestPostTransferInsufficientFunds(t *testing.T) {
	failed := sampleTransfer(domain.StatusFailed)
	failed.FailureReason = domain.ErrInsufficientFunds.Error()

	svc := &fakeService{
		transferResult: &service.TransferResult{Transfer: failed, Replayed: false},
		transferErr:    domain.ErrInsufficientFunds,
	}
	h := New(svc)

	rec := doRequest(t, h, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w-from", ToWalletID: "w-to", Amount: 999999,
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var resp failedTransferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "insufficient_funds", resp.Error.Code)
	require.NotNil(t, resp.Transfer)
	assert.Equal(t, string(domain.StatusFailed), resp.Transfer.Status)
	assert.Equal(t, "t-123", resp.Transfer.Id)
}

func TestPostTransferInsufficientFundsReplay(t *testing.T) {
	failed := sampleTransfer(domain.StatusFailed)
	svc := &fakeService{
		transferResult: &service.TransferResult{Transfer: failed, Replayed: true},
		transferErr:    domain.ErrInsufficientFunds,
	}
	h := New(svc)

	rec := doRequest(t, h, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w-from", ToWalletID: "w-to", Amount: 999999,
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "true", rec.Header().Get("Idempotent-Replayed"))
}

func TestPostTransferValidationError(t *testing.T) {
	svc := &fakeService{transferErr: domain.ErrSameWallet}
	h := New(svc)

	rec := doRequest(t, h, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w-same", ToWalletID: "w-same", Amount: 1,
	})

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid_request", resp.Error.Code)
}

func TestPostTransferMalformedJSON(t *testing.T) {
	h := New(&fakeService{})
	rec := doRequest(t, h, http.MethodPost, "/transfers", "{not json")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid_request", resp.Error.Code)
}

func TestPostTransferUnknownField(t *testing.T) {
	h := New(&fakeService{})
	rec := doRequest(t, h, http.MethodPost, "/transfers", `{"idempotencyKey":"k","extra":"nope"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPostTransferTrailingData(t *testing.T) {
	h := New(&fakeService{})
	rec := doRequest(t, h, http.MethodPost, "/transfers", `{"idempotencyKey":"k"}{"another":1}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPostTransferIdempotencyKeyReuse(t *testing.T) {
	svc := &fakeService{transferErr: domain.ErrIdempotencyKeyReuse}
	h := New(svc)

	rec := doRequest(t, h, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w-from", ToWalletID: "w-to", Amount: 500,
	})

	require.Equal(t, http.StatusConflict, rec.Code)
	var resp errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "idempotency_key_reuse", resp.Error.Code)
}

func TestPostTransferInternalError(t *testing.T) {
	svc := &fakeService{transferErr: domain.ErrInvalidStateTransition}
	h := New(svc)

	rec := doRequest(t, h, http.MethodPost, "/transfers", transferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w-from", ToWalletID: "w-to", Amount: 500,
	})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var resp errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "internal_error", resp.Error.Code)
	// Internal error message is not leaked.
	assert.NotContains(t, resp.Error.Message, domain.ErrInvalidStateTransition.Error())
}

func TestGetTransferFound(t *testing.T) {
	svc := &fakeService{getTransfer: sampleTransfer(domain.StatusProcessed)}
	h := New(svc)

	rec := doRequest(t, h, http.MethodGet, "/transfers/t-123", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp transferResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "t-123", resp.Id)
}

func TestGetTransferNotFound(t *testing.T) {
	svc := &fakeService{getTransferErr: domain.ErrTransferNotFound}
	h := New(svc)

	rec := doRequest(t, h, http.MethodGet, "/transfers/missing", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	var resp errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "not_found", resp.Error.Code)
}

func TestGetWalletBalanceFound(t *testing.T) {
	svc := &fakeService{balance: &service.WalletBalance{WalletID: "w-1", Balance: 1234}}
	h := New(svc)

	rec := doRequest(t, h, http.MethodGet, "/wallets/w-1/balance", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp walletBalanceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "w-1", resp.WalletId)
	assert.Equal(t, int64(1234), resp.Balance)
}

func TestGetWalletBalanceNotFound(t *testing.T) {
	svc := &fakeService{balanceErr: domain.ErrWalletNotFound}
	h := New(svc)

	rec := doRequest(t, h, http.MethodGet, "/wallets/missing/balance", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	var resp errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "not_found", resp.Error.Code)
}

func TestHealthz(t *testing.T) {
	h := New(&fakeService{})
	rec := doRequest(t, h, http.MethodGet, "/healthz", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
}
