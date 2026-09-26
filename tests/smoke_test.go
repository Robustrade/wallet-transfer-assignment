package tests

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"wallet/internal/api"
	"wallet/internal/constants"
	"wallet/internal/dto"
	"wallet/internal/service"
	"wallet/tests/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Run(m)) }

// newServer wires the full stack (store -> service -> HTTP router) exactly as
// cmd/api does, on top of the test database.
func newServer(t *testing.T, env *testutil.Env) *httptest.Server {
	t.Helper()
	return newServerOn(t, env.Store, env.Store.Ping)
}

// newServerOn is newServer over an arbitrary store and health check, so
// failure tests can present a store that misbehaves.
func newServerOn(t *testing.T, store service.Store, ping func(context.Context) error) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.New(store, logger)
	srv := httptest.NewServer(api.NewRouter(svc, logger, ping))
	t.Cleanup(srv.Close)
	return srv
}

// post sends a raw body to the legacy transfer URL and returns the status and
// exact response bytes, so replay tests can compare byte-for-byte.
func post(t *testing.T, srv *httptest.Server, body string) (int, []byte) {
	t.Helper()
	return postTo(t, srv, "/transfers", body)
}

func postTo(t *testing.T, srv *httptest.Server, path, body string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, raw
}

func get(t *testing.T, srv *httptest.Server, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, raw
}

// TestSmokeTransferHappyPath walks one transfer end to end: POST, balances,
// ledger, transfer lookup, wallet lookup, ledger listing, and an identical
// replay.
func TestSmokeTransferHappyPath(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	srv := newServer(t, env)

	const body = `{"idempotencyKey":"smoke-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, first := post(t, srv, body)
	if status != http.StatusCreated {
		t.Fatalf("POST /transfers: status %d, body %s", status, first)
	}

	var created dto.TransferResponse
	if err := json.Unmarshal(first, &created); err != nil {
		t.Fatalf("decode transfer response: %v", err)
	}
	if created.Status != string("PROCESSED") {
		t.Errorf("status = %q, want PROCESSED", created.Status)
	}
	if created.Amount != 100 || created.FromWalletID != "wallet_1" || created.ToWalletID != "wallet_2" {
		t.Errorf("unexpected transfer: %+v", created)
	}

	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance = %d, want 900", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 600 {
		t.Errorf("wallet_2 balance = %d, want 600", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); got != 2 {
		t.Errorf("ledger entries = %d, want 2", got)
	}

	// The identical replay returns the recorded bytes, not a second transfer.
	status, replay := post(t, srv, body)
	if status != http.StatusCreated {
		t.Fatalf("replay: status %d, body %s", status, replay)
	}
	if string(replay) != string(first) {
		t.Errorf("replay body differs:\n first: %s\nreplay: %s", first, replay)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM transfers"); got != 1 {
		t.Errorf("transfers = %d, want 1", got)
	}
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance after replay = %d, want 900", got)
	}

	// GET /transfers/{id}
	status, raw := get(t, srv, "/transfers/"+created.TransferID)
	if status != http.StatusOK {
		t.Fatalf("GET transfer: status %d, body %s", status, raw)
	}
	var fetched dto.TransferResponse
	if err := json.Unmarshal(raw, &fetched); err != nil {
		t.Fatalf("decode fetched transfer: %v", err)
	}
	if fetched.TransferID != created.TransferID || fetched.Status != created.Status {
		t.Errorf("fetched transfer = %+v, want %+v", fetched, created)
	}

	// GET /wallets/{id}
	status, raw = get(t, srv, "/wallets/wallet_1")
	if status != http.StatusOK {
		t.Fatalf("GET wallet: status %d, body %s", status, raw)
	}
	var wallet dto.WalletResponse
	if err := json.Unmarshal(raw, &wallet); err != nil {
		t.Fatalf("decode wallet: %v", err)
	}
	if wallet.WalletID != "wallet_1" || wallet.Balance != 900 {
		t.Errorf("wallet = %+v, want id wallet_1 balance 900", wallet)
	}

	// GET /wallets/{id}/ledger
	status, raw = get(t, srv, "/wallets/wallet_1/ledger?limit=10")
	if status != http.StatusOK {
		t.Fatalf("GET ledger: status %d, body %s", status, raw)
	}
	var ledger struct {
		Entries []dto.LedgerEntryResponse `json:"entries"`
	}
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatalf("decode ledger: %v", err)
	}
	if len(ledger.Entries) != 1 {
		t.Fatalf("ledger entries for wallet_1 = %d, want 1", len(ledger.Entries))
	}
	if e := ledger.Entries[0]; e.Type != "DEBIT" || e.Amount != 100 || e.TransferID != created.TransferID {
		t.Errorf("ledger entry = %+v, want DEBIT 100 of %s", e, created.TransferID)
	}
}

// TestSmokeV1RoutesAndLegacyCompatibility proves the canonical versioned API
// and unversioned compatibility aliases operate on the same transfer state.
func TestSmokeV1RoutesAndLegacyCompatibility(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	srv := newServer(t, env)

	const body = `{"idempotencyKey":"v1-smoke-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, first := postTo(t, srv, "/v1/transfers", body)
	if status != http.StatusCreated {
		t.Fatalf("POST /v1/transfers: status %d, body %s", status, first)
	}
	var created dto.TransferResponse
	if err := json.Unmarshal(first, &created); err != nil {
		t.Fatalf("decode versioned transfer response: %v", err)
	}

	// The legacy alias reaches the same idempotency record and must replay it
	// exactly instead of creating another transfer.
	status, replay := post(t, srv, body)
	if status != http.StatusCreated {
		t.Fatalf("POST /transfers replay: status %d, body %s", status, replay)
	}
	if string(replay) != string(first) {
		t.Errorf("cross-version replay differs:\n versioned: %s\n legacy: %s", first, replay)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM transfers"); got != 1 {
		t.Errorf("transfers = %d, want 1", got)
	}

	status, raw := get(t, srv, "/v1/transfers/"+created.TransferID)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/transfers/{id}: status %d, body %s", status, raw)
	}
	var fetched dto.TransferResponse
	if err := json.Unmarshal(raw, &fetched); err != nil {
		t.Fatalf("decode versioned fetched transfer: %v", err)
	}
	if fetched.TransferID != created.TransferID || fetched.Status != created.Status {
		t.Errorf("versioned fetched transfer = %+v, want %+v", fetched, created)
	}

	status, raw = get(t, srv, "/v1/wallets/wallet_1")
	if status != http.StatusOK {
		t.Fatalf("GET /v1/wallets/wallet_1: status %d, body %s", status, raw)
	}
	var wallet dto.WalletResponse
	if err := json.Unmarshal(raw, &wallet); err != nil {
		t.Fatalf("decode versioned wallet response: %v", err)
	}
	if wallet.WalletID != "wallet_1" || wallet.Balance != 900 {
		t.Errorf("versioned wallet = %+v, want id wallet_1 balance 900", wallet)
	}

	status, raw = get(t, srv, "/v1/wallets/wallet_1/ledger?limit=10")
	if status != http.StatusOK {
		t.Fatalf("GET /v1/wallets/wallet_1/ledger: status %d, body %s", status, raw)
	}
	var ledger struct {
		Entries []dto.LedgerEntryResponse `json:"entries"`
	}
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatalf("decode versioned ledger response: %v", err)
	}
	if len(ledger.Entries) != 1 || ledger.Entries[0].TransferID != created.TransferID {
		t.Errorf("versioned ledger entries = %+v, want one entry for %s", ledger.Entries, created.TransferID)
	}

	status, raw = get(t, srv, "/v1/healthz")
	if status != http.StatusOK {
		t.Errorf("GET /v1/healthz: status %d, want 200 (body %s)", status, raw)
	}
}

func TestLedgerForUnknownWalletReturnsNotFound(t *testing.T) {
	env := testutil.NewEnv(t)
	srv := newServer(t, env)

	status, raw := get(t, srv, "/v1/wallets/missing/ledger")
	if status != http.StatusNotFound {
		t.Fatalf("GET ledger for missing wallet: status %d, want 404 (body %s)", status, raw)
	}

	var response dto.ErrorResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Error.Code != constants.CodeWalletNotFound {
		t.Errorf("error code = %q, want %q", response.Error.Code, constants.CodeWalletNotFound)
	}
}

func TestTransferRejectsDestinationBalanceOverflow(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "source", 100)
	env.CreateWallet(t, "destination", int64(1<<63-1))
	srv := newServer(t, env)

	status, raw := post(t, srv, `{"idempotencyKey":"overflow-1","fromWalletId":"source","toWalletId":"destination","amount":1}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST overflow transfer: status %d, want 422 (body %s)", status, raw)
	}
	var response dto.ErrorResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Error.Code != constants.CodeBalanceOverflow {
		t.Errorf("error code = %q, want %q", response.Error.Code, constants.CodeBalanceOverflow)
	}
	if got := env.Balance(t, "source"); got != 100 {
		t.Errorf("source balance = %d, want 100", got)
	}
	if got := env.Balance(t, "destination"); got != int64(1<<63-1) {
		t.Errorf("destination balance = %d, want max int64", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); got != 0 {
		t.Errorf("ledger entries = %d, want 0", got)
	}
}

// TestSmokeKeyReuseIsRejected covers the assignment's core subtlety: the same
// idempotencyKey with a different payload must be refused, and must not move
// money again.
func TestSmokeKeyReuseIsRejected(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	srv := newServer(t, env)

	status, _ := post(t, srv, `{"idempotencyKey":"smoke-2","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if status != http.StatusCreated {
		t.Fatalf("first POST: status %d, want 201", status)
	}

	status, raw := post(t, srv, `{"idempotencyKey":"smoke-2","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":200}`)
	if status != http.StatusConflict {
		t.Fatalf("reused key: status %d, want 409 (body %s)", status, raw)
	}
	var errResp dto.ErrorResponse
	if err := json.Unmarshal(raw, &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Code != constants.CodeKeyReused {
		t.Errorf("error code = %q, want %q", errResp.Error.Code, constants.CodeKeyReused)
	}

	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance = %d, want 900 (the 409 must not move money)", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM transfers"); got != 1 {
		t.Errorf("transfers = %d, want 1", got)
	}
}

// TestSmokeValidationAndNotFound covers thin but important edges: malformed
// bodies are 400, unknown wallets are 404 with an unrecorded-key error, and
// unknown paths are 404 from the router.
func TestSmokeValidationAndNotFound(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	srv := newServer(t, env)

	cases := []struct {
		name string
		body string
		want int
		code string
	}{
		{"missing field", `{"idempotencyKey":"v-1","fromWalletId":"wallet_1","amount":100}`, http.StatusBadRequest, constants.CodeValidation},
		{"unknown field", `{"idempotencyKey":"v-2","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100,"extra":1}`, http.StatusBadRequest, constants.CodeValidation},
		{"wrong type", `{"idempotencyKey":"v-3","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":"100"}`, http.StatusBadRequest, constants.CodeValidation},
		{"malformed json", `{"idempotencyKey":`, http.StatusBadRequest, constants.CodeValidation},
		{"non-positive amount", `{"idempotencyKey":"v-4","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":0}`, http.StatusBadRequest, constants.CodeValidation},
		{"same wallet", `{"idempotencyKey":"v-5","fromWalletId":"wallet_1","toWalletId":"wallet_1","amount":100}`, http.StatusBadRequest, constants.CodeValidation},
		{"NUL in idempotencyKey", `{"idempotencyKey":"v-7\u0000","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`, http.StatusBadRequest, constants.CodeValidation},
		{"NUL in wallet id", `{"idempotencyKey":"v-8","fromWalletId":"wallet_1","toWalletId":"wallet_\u0000x","amount":100}`, http.StatusBadRequest, constants.CodeValidation},
		{"wallet not found", `{"idempotencyKey":"v-6","fromWalletId":"wallet_1","toWalletId":"wallet_9","amount":100}`, http.StatusNotFound, constants.CodeWalletNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := post(t, srv, tc.body)
			if status != tc.want {
				t.Fatalf("status %d, want %d (body %s)", status, tc.want, raw)
			}
			var errResp dto.ErrorResponse
			if err := json.Unmarshal(raw, &errResp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if errResp.Error.Code != tc.code {
				t.Errorf("error code = %q, want %q", errResp.Error.Code, tc.code)
			}
		})
	}

	if status, raw := get(t, srv, "/nope"); status != http.StatusNotFound {
		t.Errorf("GET /nope: status %d, want 404 (body %s)", status, raw)
	}
	if status, raw := get(t, srv, "/healthz"); status != http.StatusOK {
		t.Errorf("GET /healthz: status %d, want 200 (body %s)", status, raw)
	}

	// The rejected 404 was recorded: its replay is byte-identical.
	const notFound = `{"idempotencyKey":"v-6","fromWalletId":"wallet_1","toWalletId":"wallet_9","amount":100}`
	_, first := post(t, srv, notFound)
	status, replay := post(t, srv, notFound)
	if status != http.StatusNotFound || string(replay) != string(first) {
		t.Errorf("404 replay: status %d body %s, want identical 404 %s", status, replay, first)
	}

	// Validation failures never claim the key: fixing the body under the same
	// key must still work.
	status, raw := post(t, srv, `{"idempotencyKey":"v-5","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if status != http.StatusCreated {
		t.Errorf("corrected request after validation error: status %d, want 201 (body %s)", status, raw)
	}
}

// TestSmokeLedgerLimitValidation pins the ledger endpoint's limit contract: an
// absent limit selects the default page size, an explicit non-positive or
// non-numeric limit is a 400, and an oversized limit is clamped, not rejected.
func TestSmokeLedgerLimitValidation(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	srv := newServer(t, env)

	for _, tc := range []struct {
		name  string
		limit string
		want  int
	}{
		{"absent limit", "", http.StatusOK},
		{"limit zero", "0", http.StatusBadRequest},
		{"limit negative", "-3", http.StatusBadRequest},
		{"limit not a number", "abc", http.StatusBadRequest},
		{"oversized limit is clamped", "1000000", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/wallets/wallet_1/ledger"
			if tc.limit != "" {
				path += "?limit=" + tc.limit
			}
			status, raw := get(t, srv, path)
			if status != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", status, tc.want, raw)
			}
			if tc.want == http.StatusBadRequest {
				var errResp dto.ErrorResponse
				if err := json.Unmarshal(raw, &errResp); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if errResp.Error.Code != constants.CodeValidation {
					t.Errorf("error code = %q, want %q", errResp.Error.Code, constants.CodeValidation)
				}
			}
		})
	}
}
