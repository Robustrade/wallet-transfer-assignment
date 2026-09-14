package handler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/dbtest"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/handler"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/repository"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/service"
)

// newServer starts a real HTTP server over the real service and a real
// database. Nothing is mocked: these tests exercise the whole stack, because
// the interactions between layers are where this system's bugs have actually
// been.
func newServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.Pool(t)
	transfers := handler.NewTransfers(service.NewTransfers(repository.New(pool), nil), nil)

	server := httptest.NewServer(transfers.Routes())
	t.Cleanup(server.Close)

	return server, pool
}

// post sends a raw body so tests can send malformed JSON, and returns the
// status, headers and body bytes exactly as the client would see them.
func post(t *testing.T, server *httptest.Server, path, body string) (int, http.Header, []byte) {
	t.Helper()

	response, err := server.Client().Post(
		server.URL+path, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	return response.StatusCode, response.Header, payload
}

func transferBody(key, from, to string, amount int64) string {
	return fmt.Sprintf(
		`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":%d}`,
		key, from, to, amount)
}

func key() string { return "key_" + uuid.NewString() }

func decode(t *testing.T, payload []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode response %s: %v", payload, err)
	}

	return decoded
}

func TestCreateTransferReturns201WithTheTransfer(t *testing.T) {
	server, pool := newServer(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	idempotencyKey := key()

	status, header, payload := post(t, server, "/transfers", transferBody(idempotencyKey, from, to, 300))

	if status != http.StatusCreated {
		t.Fatalf("status is %d, want 201: %s", status, payload)
	}
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type is %q, want application/json", got)
	}
	if got := header.Get("Idempotent-Replay"); got != "" {
		t.Errorf("a first request carried Idempotent-Replay: %q", got)
	}

	body := decode(t, payload)
	for field, want := range map[string]any{
		"idempotencyKey": idempotencyKey,
		"fromWalletId":   from,
		"toWalletId":     to,
		"amount":         float64(300),
		"state":          "PROCESSED",
		"failureReason":  nil,
	} {
		if body[field] != want {
			t.Errorf("%s is %v, want %v", field, body[field], want)
		}
	}
	if body["id"] == "" || body["id"] == nil {
		t.Error("response carries no transfer id")
	}
	if body["createdAt"] == nil {
		t.Error("response carries no createdAt")
	}

	if got := dbtest.Balance(t, pool, from); got != 700 {
		t.Errorf("source holds %d, want 700", got)
	}
}

// Exactly-once at the API level: the second call must describe the same
// transfer, byte for byte.
func TestReplayReturns200WithAnIdenticalBody(t *testing.T) {
	server, pool := newServer(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	body := transferBody(key(), from, to, 300)

	firstStatus, _, firstPayload := post(t, server, "/transfers", body)
	if firstStatus != http.StatusCreated {
		t.Fatalf("first status is %d, want 201: %s", firstStatus, firstPayload)
	}

	secondStatus, secondHeader, secondPayload := post(t, server, "/transfers", body)

	if secondStatus != http.StatusOK {
		t.Errorf("replay status is %d, want 200", secondStatus)
	}
	if got := secondHeader.Get("Idempotent-Replay"); got != "true" {
		t.Errorf("Idempotent-Replay is %q, want true", got)
	}
	if !bytes.Equal(firstPayload, secondPayload) {
		t.Errorf("replay body differs:\n first: %s\nsecond: %s", firstPayload, secondPayload)
	}

	if got := dbtest.Balance(t, pool, from); got != 700 {
		t.Errorf("source holds %d after a replay, want 700 — money moved twice", got)
	}
}

// A failure is 422 on the first call and on every replay. Downgrading a
// replayed failure to 200 would invite a client to read it as a success.
func TestInsufficientFundsReturns422BothTimes(t *testing.T) {
	server, pool := newServer(t)
	from := dbtest.Wallet(t, pool, 100)
	to := dbtest.Wallet(t, pool, 0)
	body := transferBody(key(), from, to, 500)

	status, _, payload := post(t, server, "/transfers", body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status is %d, want 422: %s", status, payload)
	}

	decoded := decode(t, payload)
	if decoded["state"] != "FAILED" {
		t.Errorf("state is %v, want FAILED", decoded["state"])
	}
	if decoded["failureReason"] != "insufficient funds" {
		t.Errorf("failureReason is %v, want %q", decoded["failureReason"], "insufficient funds")
	}

	replayStatus, replayHeader, replayPayload := post(t, server, "/transfers", body)
	if replayStatus != http.StatusUnprocessableEntity {
		t.Errorf("replayed failure status is %d, want 422", replayStatus)
	}
	if got := replayHeader.Get("Idempotent-Replay"); got != "true" {
		t.Errorf("Idempotent-Replay is %q, want true", got)
	}
	if !bytes.Equal(payload, replayPayload) {
		t.Errorf("replayed failure body differs:\n first: %s\nsecond: %s", payload, replayPayload)
	}

	if got := dbtest.Balance(t, pool, from); got != 100 {
		t.Errorf("source holds %d, want 100 — a failure must move no money", got)
	}
}

func TestUnknownWalletReturns404NamingIt(t *testing.T) {
	server, pool := newServer(t)
	to := dbtest.Wallet(t, pool, 0)

	status, _, payload := post(t, server, "/transfers",
		transferBody(key(), "wallet_missing", to, 100))

	if status != http.StatusNotFound {
		t.Fatalf("status is %d, want 404: %s", status, payload)
	}

	body := decode(t, payload)["error"].(map[string]any)
	if body["code"] != "WALLET_NOT_FOUND" {
		t.Errorf("code is %v, want WALLET_NOT_FOUND", body["code"])
	}
	if message, _ := body["message"].(string); !bytes.Contains([]byte(message), []byte("wallet_missing")) {
		t.Errorf("message %q does not name the missing wallet", message)
	}
}

func TestSameKeyWithDifferentParametersReturns409(t *testing.T) {
	server, pool := newServer(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	idempotencyKey := key()

	if status, _, payload := post(t, server, "/transfers",
		transferBody(idempotencyKey, from, to, 300)); status != http.StatusCreated {
		t.Fatalf("first status is %d, want 201: %s", status, payload)
	}

	status, _, payload := post(t, server, "/transfers",
		transferBody(idempotencyKey, from, to, 301))

	if status != http.StatusConflict {
		t.Fatalf("status is %d, want 409: %s", status, payload)
	}
	if code := decode(t, payload)["error"].(map[string]any)["code"]; code != "IDEMPOTENCY_KEY_CONFLICT" {
		t.Errorf("code is %v, want IDEMPOTENCY_KEY_CONFLICT", code)
	}
	if got := dbtest.Balance(t, pool, from); got != 700 {
		t.Errorf("source holds %d after a refused request, want 700", got)
	}
}

func TestMalformedRequestsReturn400(t *testing.T) {
	server, pool := newServer(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)

	tests := map[string]string{
		"not json":          `{`,
		"empty body":        ``,
		"unknown field":     fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":100,"ammount":5}`, key(), from, to),
		"fractional amount": fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":100.5}`, key(), from, to),
		"amount as string":  fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":"100"}`, key(), from, to),
		"two json objects":  transferBody(key(), from, to, 100) + transferBody(key(), from, to, 100),
		"zero amount":       transferBody(key(), from, to, 0),
		"negative amount":   transferBody(key(), from, to, -100),
		"missing key":       fmt.Sprintf(`{"fromWalletId":%q,"toWalletId":%q,"amount":100}`, from, to),
		"self transfer":     transferBody(key(), from, from, 100),
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			status, _, payload := post(t, server, "/transfers", body)

			if status != http.StatusBadRequest {
				t.Fatalf("status is %d, want 400: %s", status, payload)
			}
			if code := decode(t, payload)["error"].(map[string]any)["code"]; code != "INVALID_REQUEST" {
				t.Errorf("code is %v, want INVALID_REQUEST", code)
			}
			if got := dbtest.Balance(t, pool, from); got != 1000 {
				t.Errorf("a rejected request moved money: balance is %d", got)
			}
		})
	}
}

// A validation failure names the field at fault, so a client can fix it without
// guessing.
func TestValidationFailureNamesTheField(t *testing.T) {
	server, pool := newServer(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)

	_, _, payload := post(t, server, "/transfers", transferBody(key(), from, to, 0))

	if field := decode(t, payload)["error"].(map[string]any)["field"]; field != "amount" {
		t.Errorf("field is %v, want amount", field)
	}
}

func TestWrongMethodAndPathAreRejected(t *testing.T) {
	server, _ := newServer(t)

	tests := map[string]struct {
		method, path string
		want         int
	}{
		"GET /transfers":    {http.MethodGet, "/transfers", http.StatusMethodNotAllowed},
		"DELETE /transfers": {http.MethodDelete, "/transfers", http.StatusMethodNotAllowed},
		"POST /unknown":     {http.MethodPost, "/unknown", http.StatusNotFound},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequest(tc.method, server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}

			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("send request: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != tc.want {
				t.Errorf("status is %d, want %d", response.StatusCode, tc.want)
			}
		})
	}
}
