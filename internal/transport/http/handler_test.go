package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/memory"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	httptransport "github.com/Robustrade/wallet-transfer-assignment/internal/transport/http"
)

// newTestServer wires the full stack (memory store -> services -> handler ->
// router) and returns an httptest server exercising the real HTTP contract.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := memory.NewStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	transferSvc := service.NewTransferService(store, nil, logger)
	walletSvc := service.NewWalletService(store, nil, logger)
	handler := httptransport.NewHandler(transferSvc, walletSvc, logger)
	srv := httptest.NewServer(httptransport.NewRouter(handler))
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer closeBody(resp)
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// closeBody drains and closes a response body, satisfying linters that require
// the Close error to be handled.
func closeBody(resp *http.Response) {
	_ = resp.Body.Close()
}

// seedWallet creates a wallet with the given opening balance and returns its
// server-generated id, so tests reference real ids instead of guessing them.
func seedWallet(t *testing.T, baseURL string, balance int64) string {
	t.Helper()
	resp := doJSON(t, http.MethodPost, baseURL+"/api/v1/wallets", map[string]any{
		"openingBalance": balance, "currency": "USD",
	})
	defer closeBody(resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed wallet: status %d", resp.StatusCode)
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode seed wallet response: %v", err)
	}
	if body.ID == "" {
		t.Fatalf("seed wallet: server returned empty id")
	}
	return body.ID
}

func TestAPI_CreateTransfer_Success(t *testing.T) {
	srv := newTestServer(t)
	from := seedWallet(t, srv.URL, 1000)
	to := seedWallet(t, srv.URL, 0)

	resp := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", map[string]any{
		"idempotencyKey": "abc123", "fromWalletId": from, "toWalletId": to, "amount": 100,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var body map[string]any
	decode(t, resp, &body)
	if body["status"] != "PROCESSED" {
		t.Fatalf("expected PROCESSED, got %v", body["status"])
	}
}

func TestAPI_CreateTransfer_DuplicateReturns200AndSameResult(t *testing.T) {
	srv := newTestServer(t)
	from := seedWallet(t, srv.URL, 1000)
	to := seedWallet(t, srv.URL, 0)

	payload := map[string]any{
		"idempotencyKey": "dup-key", "fromWalletId": from, "toWalletId": to, "amount": 100,
	}

	first := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", payload)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first call expected 201, got %d", first.StatusCode)
	}
	var firstBody map[string]any
	decode(t, first, &firstBody)

	second := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", payload)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("duplicate expected 200, got %d", second.StatusCode)
	}
	var secondBody map[string]any
	decode(t, second, &secondBody)

	if firstBody["id"] != secondBody["id"] {
		t.Fatalf("duplicate must return same transfer id: %v != %v", firstBody["id"], secondBody["id"])
	}

	// Balance moved only once.
	wResp := doJSON(t, http.MethodGet, srv.URL+"/api/v1/wallets/"+from, nil)
	var wallet map[string]any
	decode(t, wResp, &wallet)
	if wallet["balance"].(float64) != 900 {
		t.Fatalf("expected balance 900 after dedup, got %v", wallet["balance"])
	}
}

func TestAPI_CreateTransfer_KeyConflictReturns409(t *testing.T) {
	srv := newTestServer(t)
	from := seedWallet(t, srv.URL, 1000)
	to := seedWallet(t, srv.URL, 0)

	first := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", map[string]any{
		"idempotencyKey": "k", "fromWalletId": from, "toWalletId": to, "amount": 100,
	})
	closeBody(first)

	conflict := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", map[string]any{
		"idempotencyKey": "k", "fromWalletId": from, "toWalletId": to, "amount": 999,
	})
	defer closeBody(conflict)
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", conflict.StatusCode)
	}
}

func TestAPI_CreateTransfer_ValidationErrors(t *testing.T) {
	srv := newTestServer(t)

	tests := []struct {
		name    string
		payload map[string]any
	}{
		{"missing key", map[string]any{"fromWalletId": "w1", "toWalletId": "w2", "amount": 100}},
		{"same wallet", map[string]any{"idempotencyKey": "k", "fromWalletId": "w1", "toWalletId": "w1", "amount": 100}},
		{"zero amount", map[string]any{"idempotencyKey": "k", "fromWalletId": "w1", "toWalletId": "w2", "amount": 0}},
		{"negative amount", map[string]any{"idempotencyKey": "k", "fromWalletId": "w1", "toWalletId": "w2", "amount": -5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", tc.payload)
			defer closeBody(resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestAPI_CreateTransfer_UnknownFieldRejected(t *testing.T) {
	srv := newTestServer(t)
	resp := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", map[string]any{
		"idempotencyKey": "k", "fromWalletId": "w1", "toWalletId": "w2", "amount": 100, "unexpected": true,
	})
	defer closeBody(resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", resp.StatusCode)
	}
}

func TestAPI_GetTransfer_NotFound(t *testing.T) {
	srv := newTestServer(t)
	resp := doJSON(t, http.MethodGet, srv.URL+"/api/v1/transfers/does-not-exist", nil)
	defer closeBody(resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPI_WalletLedgerHistory(t *testing.T) {
	srv := newTestServer(t)
	from := seedWallet(t, srv.URL, 1000)
	to := seedWallet(t, srv.URL, 0)

	transfer := doJSON(t, http.MethodPost, srv.URL+"/api/v1/transfers", map[string]any{
		"idempotencyKey": "k", "fromWalletId": from, "toWalletId": to, "amount": 100,
	})
	closeBody(transfer)

	resp := doJSON(t, http.MethodGet, srv.URL+"/api/v1/wallets/"+from+"/ledger", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		WalletID string `json:"walletId"`
		Entries  []struct {
			Type   string `json:"type"`
			Amount int64  `json:"amount"`
		} `json:"entries"`
	}
	decode(t, resp, &body)
	if len(body.Entries) != 1 || body.Entries[0].Type != "DEBIT" || body.Entries[0].Amount != 100 {
		t.Fatalf("unexpected ledger history: %+v", body)
	}
}

func TestAPI_Health(t *testing.T) {
	srv := newTestServer(t)
	resp := doJSON(t, http.MethodGet, srv.URL+"/healthz", nil)
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
