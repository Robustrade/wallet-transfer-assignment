package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/priiyank-xd/wallet-transfer-service/internal/domain"
	"github.com/priiyank-xd/wallet-transfer-service/internal/repository"
	"github.com/priiyank-xd/wallet-transfer-service/internal/service"
	"github.com/priiyank-xd/wallet-transfer-service/internal/testutil"
)

func newTestHandler(t *testing.T) (*TransferHandler, *pgxpool.Pool) {
	t.Helper()

	pool := testutil.Setup(t)
	repo := repository.New(pool)
	svc := service.NewTransferService(repo)
	return NewTransferHandler(svc), pool
}

func postTransfer(t *testing.T, h *TransferHandler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateTransfer(rec, req)
	return rec
}

func TestCreateTransferHandlerSuccess(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(domain.TransferRequest{
		IdempotencyKey: testutil.NewIdempotencyKey(t),
		FromWalletID:   testutil.WalletAlice,
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	})

	rec := postTransfer(t, h, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json content type, got %s", ct)
	}

	var resp domain.TransferResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != domain.PROCESSED {
		t.Fatalf("expected PROCESSED, got %s", resp.Status)
	}
}

func TestCreateTransferHandlerValidation(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(domain.TransferRequest{
		FromWalletID: testutil.WalletAlice,
		ToWalletID:   testutil.WalletBob,
		Amount:       100,
	})

	rec := postTransfer(t, h, string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransferHandlerInvalidJSON(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := postTransfer(t, h, "{bad json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransferHandlerEmptyBody(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(nil))
	rec := httptest.NewRecorder()
	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransferHandlerWalletNotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(domain.TransferRequest{
		IdempotencyKey: testutil.NewIdempotencyKey(t),
		FromWalletID:   "does-not-exist",
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	})

	rec := postTransfer(t, h, string(body))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateTransferHandlerInsufficientFunds(t *testing.T) {
	h, pool := newTestHandler(t)
	testutil.SetWalletBalance(t, pool, testutil.WalletAlice, 50)

	body, _ := json.Marshal(domain.TransferRequest{
		IdempotencyKey: testutil.NewIdempotencyKey(t),
		FromWalletID:   testutil.WalletAlice,
		ToWalletID:     testutil.WalletBob,
		Amount:         100,
	})

	rec := postTransfer(t, h, string(body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp domain.TransferResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != domain.ErrInsufficientFunds.Error() {
		t.Fatalf("expected insufficient funds error, got %q", resp.Error)
	}
}
