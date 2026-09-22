package router_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/router"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/testutil"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	uow := testutil.NewMockUnitOfWork()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	transfers := service.NewTransferService(uow, service.WithLogger(logger))
	wallets := service.NewWalletService(uow, service.WithLogger(logger))
	return router.New(transfers, wallets, logger)
}

func doJSON(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTransferEndpoint_EndToEnd(t *testing.T) {
	r := newTestRouter(t)

	doJSON(t, r, http.MethodPost, "/wallets", map[string]any{"id": "wallet_1", "initialBalance": 500})
	doJSON(t, r, http.MethodPost, "/wallets", map[string]any{"id": "wallet_2", "initialBalance": 0})

	body := map[string]any{
		"idempotencyKey": "http-key-1",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	}

	first := doJSON(t, r, http.MethodPost, "/transfers", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first transfer status = %d, want 201, body=%s", first.Code, first.Body.String())
	}

	second := doJSON(t, r, http.MethodPost, "/transfers", body)
	if second.Code != http.StatusCreated {
		t.Fatalf("replayed transfer status = %d, want 201, body=%s", second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("expected Idempotent-Replayed header on the replayed response")
	}

	balance := doJSON(t, r, http.MethodGet, "/wallets/wallet_1/balance", nil)
	var walletBody struct {
		Balance int64 `json:"balance"`
	}
	if err := json.Unmarshal(balance.Body.Bytes(), &walletBody); err != nil {
		t.Fatalf("decode balance response: %v", err)
	}
	if walletBody.Balance != 400 {
		t.Errorf("wallet_1 balance = %d, want 400 (duplicate must not double-debit)", walletBody.Balance)
	}

	var transferBody struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &transferBody); err != nil {
		t.Fatalf("decode transfer response: %v", err)
	}
	getTransfer := doJSON(t, r, http.MethodGet, "/transfers/"+transferBody.ID, nil)
	if getTransfer.Code != http.StatusOK {
		t.Fatalf("get transfer status = %d, want 200, body=%s", getTransfer.Code, getTransfer.Body.String())
	}

	history := doJSON(t, r, http.MethodGet, "/wallets/wallet_1/transfers", nil)
	if history.Code != http.StatusOK {
		t.Fatalf("list history status = %d, want 200, body=%s", history.Code, history.Body.String())
	}
}

func TestTransferEndpoint_UnknownWalletReturns404(t *testing.T) {
	r := newTestRouter(t)
	doJSON(t, r, http.MethodPost, "/wallets", map[string]any{"id": "wallet_1", "initialBalance": 500})

	resp := doJSON(t, r, http.MethodPost, "/transfers", map[string]any{
		"idempotencyKey": "missing-wallet",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "does-not-exist",
		"amount":         10,
	})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", resp.Code, resp.Body.String())
	}
}

func TestTransferEndpoint_InvalidAmountReturns400(t *testing.T) {
	r := newTestRouter(t)
	doJSON(t, r, http.MethodPost, "/wallets", map[string]any{"id": "wallet_1", "initialBalance": 500})
	doJSON(t, r, http.MethodPost, "/wallets", map[string]any{"id": "wallet_2", "initialBalance": 0})

	resp := doJSON(t, r, http.MethodPost, "/transfers", map[string]any{
		"idempotencyKey": "bad-amount",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         0,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", resp.Code, resp.Body.String())
	}
}

func TestRouter_CoversWalletErrorsAndHealth(t *testing.T) {
	r := newTestRouter(t)

	health := doJSON(t, r, http.MethodGet, "/healthz", nil)
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	badJSON := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBufferString("{"))
	badJSON.Header.Set("Content-Type", "application/json")
	badJSONResponse := httptest.NewRecorder()
	r.ServeHTTP(badJSONResponse, badJSON)
	if badJSONResponse.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status = %d, want 400", badJSONResponse.Code)
	}

	created := doJSON(t, r, http.MethodPost, "/wallets", map[string]any{"id": "wallet_1", "initialBalance": 10})
	if created.Code != http.StatusCreated {
		t.Fatalf("create wallet status = %d, want 201", created.Code)
	}

	duplicate := doJSON(t, r, http.MethodPost, "/wallets", map[string]any{"id": "wallet_1", "initialBalance": 10})
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate wallet status = %d, want 409", duplicate.Code)
	}

	missingBalance := doJSON(t, r, http.MethodGet, "/wallets/missing/balance", nil)
	if missingBalance.Code != http.StatusNotFound {
		t.Fatalf("missing balance status = %d, want 404", missingBalance.Code)
	}

	missingHistory := doJSON(t, r, http.MethodGet, "/wallets/missing/transfers", nil)
	if missingHistory.Code != http.StatusNotFound {
		t.Fatalf("missing history status = %d, want 404", missingHistory.Code)
	}

	missingTransfer := doJSON(t, r, http.MethodGet, "/transfers/missing", nil)
	if missingTransfer.Code != http.StatusNotFound {
		t.Fatalf("missing transfer status = %d, want 404", missingTransfer.Code)
	}
}
