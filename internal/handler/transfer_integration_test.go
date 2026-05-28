//go:build integration

package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/handler"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	d := testdb.Get(t)
	walletRepo := postgres.NewWalletRepo()
	transferRepo := postgres.NewTransferRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idemRepo := postgres.NewIdempotencyRepo()
	txm := postgres.NewTxManager(d)

	tsvc := service.NewTransferService(d, txm, walletRepo, transferRepo, ledgerRepo, idemRepo)
	wsvc := service.NewWalletService(d, walletRepo)

	mux := handler.NewRouter(
		handler.NewTransferHandler(tsvc),
		handler.NewWalletHandler(wsvc, tsvc),
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	return resp
}

// TestHTTP_TransferHappyAndReplay exercises the full HTTP stack
// end-to-end. The test spins up a real httptest.Server backed by a
// real Postgres (via testdb) and:
//
//  1. seeds two wallets,
//  2. submits a transfer,
//  3. submits the SAME transfer body with the SAME key,
//  4. asserts the second call returned the SAME id (idempotent
//     replay observable from the wire), and
//  5. asserts the wallet balance was debited exactly once.
//
// If this passes, the assignment's headline requirement
// ("exactly-once at the API level when an idempotencyKey is
// provided") is observably true from a client's perspective.
func TestHTTP_TransferHappyAndReplay(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)

	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w1", "balance": 500}))
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w2", "balance": 0}))

	body := map[string]any{
		"idempotencyKey": "k1",
		"fromWalletId":   "w1",
		"toWalletId":     "w2",
		"amount":         200,
	}
	r1 := postJSON(t, srv.URL+"/transfers", body)
	mustOK(t, r1)
	var t1 map[string]any
	_ = json.NewDecoder(r1.Body).Decode(&t1)
	r1.Body.Close()

	if t1["state"] != "PROCESSED" {
		t.Fatalf("state: %v", t1["state"])
	}

	r2 := postJSON(t, srv.URL+"/transfers", body)
	mustOK(t, r2)
	var t2 map[string]any
	_ = json.NewDecoder(r2.Body).Decode(&t2)
	r2.Body.Close()

	if t1["id"] != t2["id"] {
		t.Fatalf("replay returned different id: %v vs %v", t1["id"], t2["id"])
	}

	// Source wallet should reflect a single debit.
	wresp, err := http.Get(srv.URL + "/wallets/w1")
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, wresp)
	var w map[string]any
	_ = json.NewDecoder(wresp.Body).Decode(&w)
	wresp.Body.Close()
	if int(w["balance"].(float64)) != 300 {
		t.Fatalf("balance: %v", w["balance"])
	}
}

// TestHTTP_TransferConflict verifies that the conflict path
// (domain.ErrIdempotencyConflict) surfaces as HTTP 409 to the
// client — proves the service error is propagated through the
// handler's classify step correctly.
func TestHTTP_TransferConflict(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w1", "balance": 500}))
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w2", "balance": 0}))

	mustOK(t, postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "k1", "fromWalletId": "w1", "toWalletId": "w2", "amount": 50,
	}))
	resp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "k1", "fromWalletId": "w1", "toWalletId": "w2", "amount": 999,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d want 409", resp.StatusCode)
	}
}

// TestHTTP_TransferValidationErrors covers the three documented
// validation failure modes through the HTTP boundary. Each must
// return 400. This is the "is classify wired correctly" check at
// the validation tier.
func TestHTTP_TransferValidationErrors(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing key", map[string]any{"fromWalletId": "a", "toWalletId": "b", "amount": 1}, http.StatusBadRequest},
		{"zero amount", map[string]any{"idempotencyKey": "k", "fromWalletId": "a", "toWalletId": "b", "amount": 0}, http.StatusBadRequest},
		{"same wallet", map[string]any{"idempotencyKey": "k", "fromWalletId": "a", "toWalletId": "a", "amount": 1}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := postJSON(t, srv.URL+"/transfers", c.body)
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status: got %d want %d", resp.StatusCode, c.want)
			}
		})
	}
}

// TestHTTP_GetWalletNotFound exercises the 404 mapping for missing
// wallets. The handler returns a generic "not found" body — no
// leak of which table or which constraint triggered it.
func TestHTTP_GetWalletNotFound(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	resp, err := http.Get(srv.URL + "/wallets/ghost")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestHTTP_TransferReferencingMissingWallet checks the FK-violation
// path through the HTTP boundary: when the destination wallet does
// not exist, the response is a 4xx (which 4xx is implementation-
// dependent and not asserted here — we only check we never silently
// 200/500).
func TestHTTP_TransferReferencingMissingWallet(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w1", "balance": 10}))

	resp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "k", "fromWalletId": "w1", "toWalletId": "ghost", "amount": 1,
	})
	defer resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("expected 4xx, got %d", resp.StatusCode)
	}
}

// TestHTTP_InvalidJSON verifies that a malformed JSON body produces
// a 400, not a panic or a 500. The body sent is truncated mid-string
// to trigger the decoder's syntax error path.
func TestHTTP_InvalidJSON(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/transfers",
		bytes.NewBufferString(`{"idempotencyKey":`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestHTTP_UnknownJSONField verifies DisallowUnknownFields is
// active. A typo like "surpriseUnknown" must be rejected with 400
// — silently ignoring unknown fields would mask client bugs
// (e.g. "amunt" instead of "amount" sending 0 by default).
func TestHTTP_UnknownJSONField(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	resp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey":  "k",
		"fromWalletId":    "a",
		"toWalletId":      "b",
		"amount":          1,
		"surpriseUnknown": "should be rejected",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestHTTP_CreateWalletNegativeBalance exercises the wallet seed
// validation: negative initial balances are 400, not a 500 from a
// CHECK violation deep in the DB.
func TestHTTP_CreateWalletNegativeBalance(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	resp := postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w", "balance": -1})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestHTTP_CreateWalletInvalidJSON is the wallet-side counterpart
// to TestHTTP_InvalidJSON: malformed JSON on POST /wallets is a 400,
// not a 500.
func TestHTTP_CreateWalletInvalidJSON(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/wallets",
		bytes.NewBufferString(`not json`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func mustOK(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode >= 400 {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d: %s", resp.StatusCode, b.String())
	}
}
