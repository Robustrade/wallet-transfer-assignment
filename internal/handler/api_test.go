package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
)

type api struct {
	t      *testing.T
	server *httptest.Server
}

func setupAPI(t *testing.T) api {
	conn := testutil.DB(t)
	srv := httptest.NewServer(handler.NewRouter(conn, nil))
	t.Cleanup(srv.Close)
	a := api{t: t, server: srv}
	a.do(http.MethodPost, "/wallets", `{"id":"wallet_1","initialBalance":1000}`)
	a.do(http.MethodPost, "/wallets", `{"id":"wallet_2"}`)
	return a
}

type response struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (a api) do(method, path, body string) response {
	a.t.Helper()
	req, err := http.NewRequest(method, a.server.URL+path, strings.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()

	var raw strings.Builder
	var parsed map[string]any
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&parsed); err != nil {
		a.t.Fatalf("decode body: %v", err)
	}
	b, _ := json.Marshal(parsed)
	raw.Write(b)
	return response{status: res.StatusCode, header: res.Header, body: parsed, raw: raw.String()}
}

const transferBody = `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

func errorCode(r response) string {
	e, _ := r.body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestPostTransfer_Returns201WithProcessedTransfer(t *testing.T) {
	a := setupAPI(t)
	res := a.do(http.MethodPost, "/transfers", transferBody)

	if res.status != http.StatusCreated || res.body["status"] != "PROCESSED" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	if res.header.Get("Idempotent-Replayed") != "false" {
		t.Fatalf("Idempotent-Replayed = %q", res.header.Get("Idempotent-Replayed"))
	}
}

func TestPostTransfer_ReplayReturnsSameStatusAndBody(t *testing.T) {
	a := setupAPI(t)
	first := a.do(http.MethodPost, "/transfers", transferBody)
	second := a.do(http.MethodPost, "/transfers", transferBody)

	if second.status != first.status || second.raw != first.raw {
		t.Fatalf("replay differs:\n%d %s\n%d %s", first.status, first.raw, second.status, second.raw)
	}
	if second.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("Idempotent-Replayed = %q", second.header.Get("Idempotent-Replayed"))
	}
}

func TestPostTransfer_InsufficientFundsIs422AndReplayedAs422(t *testing.T) {
	a := setupAPI(t)
	body := strings.Replace(transferBody, `"amount":100`, `"amount":5000`, 1)
	first := a.do(http.MethodPost, "/transfers", body)
	second := a.do(http.MethodPost, "/transfers", body)

	if first.status != http.StatusUnprocessableEntity || first.body["failureReason"] != "INSUFFICIENT_FUNDS" {
		t.Fatalf("got %d %s", first.status, first.raw)
	}
	if second.status != first.status || second.raw != first.raw {
		t.Fatalf("replay differs: %d %s", second.status, second.raw)
	}
}

func TestPostTransfer_KeyReusedWithOtherBodyIs409(t *testing.T) {
	a := setupAPI(t)
	a.do(http.MethodPost, "/transfers", transferBody)
	res := a.do(http.MethodPost, "/transfers", strings.Replace(transferBody, `"amount":100`, `"amount":999`, 1))

	if res.status != http.StatusConflict || errorCode(res) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
}

func TestPostTransfer_UnknownWalletIs404(t *testing.T) {
	a := setupAPI(t)
	res := a.do(http.MethodPost, "/transfers", strings.Replace(transferBody, "wallet_2", "ghost", 1))
	if res.status != http.StatusNotFound || errorCode(res) != "WALLET_NOT_FOUND" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
}

func TestPostTransfer_InvalidBodiesAre400(t *testing.T) {
	a := setupAPI(t)
	cases := map[string]string{
		"missing key":    `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`,
		"string amount":  `{"idempotencyKey":"k","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":"100"}`,
		"decimal amount": `{"idempotencyKey":"k","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10.5}`,
		"zero amount":    `{"idempotencyKey":"k","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":0}`,
		"same wallet":    `{"idempotencyKey":"k","fromWalletId":"wallet_1","toWalletId":"wallet_1","amount":1}`,
		"unknown field":  `{"idempotencyKey":"k","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1,"currency":"INR"}`,
		"not json":       `hello`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			res := a.do(http.MethodPost, "/transfers", body)
			if res.status != http.StatusBadRequest || errorCode(res) != "VALIDATION_ERROR" {
				t.Fatalf("got %d %s", res.status, res.raw)
			}
		})
	}
}

func TestReadEndpoints_ShowBalanceLedgerAndTransfer(t *testing.T) {
	a := setupAPI(t)
	created := a.do(http.MethodPost, "/transfers", transferBody)
	id, _ := created.body["id"].(string)

	if w := a.do(http.MethodGet, "/wallets/wallet_1", ""); w.body["balance"] != float64(900) {
		t.Fatalf("wallet_1 = %s", w.raw)
	}

	ledger := a.do(http.MethodGet, "/wallets/wallet_2/ledger", "")
	entries, _ := ledger.body["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("ledger = %s", ledger.raw)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["transferId"] != id || entry["type"] != "CREDIT" || entry["amount"] != float64(100) {
		t.Fatalf("entry = %v", entry)
	}

	if got := a.do(http.MethodGet, "/transfers/"+id, ""); got.raw != created.raw {
		t.Fatalf("GET transfer = %s, want %s", got.raw, created.raw)
	}
}

func TestReadEndpoints_UnknownIDsAre404(t *testing.T) {
	a := setupAPI(t)
	if r := a.do(http.MethodGet, "/wallets/ghost", ""); r.status != http.StatusNotFound {
		t.Fatalf("wallet: %d", r.status)
	}
	if r := a.do(http.MethodGet, "/transfers/not-a-uuid", ""); r.status != http.StatusNotFound {
		t.Fatalf("malformed transfer id: %d", r.status)
	}
	if r := a.do(http.MethodGet, "/transfers/00000000-0000-4000-8000-000000000000", ""); r.status != http.StatusNotFound {
		t.Fatalf("transfer: %d", r.status)
	}
}

func TestHealth(t *testing.T) {
	a := setupAPI(t)
	if r := a.do(http.MethodGet, "/health", ""); r.status != http.StatusOK || r.body["status"] != "ok" {
		t.Fatalf("got %d %s", r.status, r.raw)
	}
}
