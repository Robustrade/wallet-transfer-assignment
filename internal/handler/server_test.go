package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/handler"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/repository"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/service"
)

func newHandler(t *testing.T, wallets map[string]int64) http.Handler {
	t.Helper()
	store, err := repository.Open(repository.MemoryDSN(t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	svc := service.New(store, nil)
	ctx := context.Background()
	for id, bal := range wallets {
		if _, err := svc.CreateWallet(ctx, id, bal); err != nil {
			t.Fatalf("create wallet %s: %v", id, err)
		}
	}
	return handler.New(svc).Routes()
}

func postJSON(t *testing.T, h http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHTTPTransferLifecycle(t *testing.T) {
	h := newHandler(t, map[string]int64{"wallet_1": 200, "wallet_2": 0})
	payload := map[string]any{
		"idempotencyKey": "abc123",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         75,
	}
	first := postJSON(t, h, payload)
	if first.Code != http.StatusCreated {
		t.Fatalf("create transfer: %d %s", first.Code, first.Body.String())
	}
	replay := postJSON(t, h, payload)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	var created, replayed map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatal(err)
	}
	if created["id"] != replayed["id"] || created["status"] != "PROCESSED" {
		t.Fatalf("created=%v replayed=%v", created, replayed)
	}
}

func TestHTTPValidationAndFailures(t *testing.T) {
	h := newHandler(t, map[string]int64{"a": 10, "b": 0})

	if rec := postJSON(t, h, map[string]any{
		"fromWalletId": "a", "toWalletId": "b", "amount": 1,
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key: %d", rec.Code)
	}
	if rec := postJSON(t, h, map[string]any{
		"idempotencyKey": "k1", "fromWalletId": "a", "toWalletId": "a", "amount": 1,
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("same wallet: %d", rec.Code)
	}
	if rec := postJSON(t, h, map[string]any{
		"idempotencyKey": "k2", "fromWalletId": "a", "toWalletId": "missing", "amount": 1,
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown wallet: %d", rec.Code)
	}
	if rec := postJSON(t, h, map[string]any{
		"idempotencyKey": "k3", "fromWalletId": "a", "toWalletId": "b", "amount": 50,
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("insufficient: %d %s", rec.Code, rec.Body.String())
	}
}
