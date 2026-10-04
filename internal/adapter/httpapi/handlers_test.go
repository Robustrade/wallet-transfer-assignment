package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/app/transfer"
	"github.com/amantiwari/wallet-transfer/internal/app/wallet"
	"github.com/amantiwari/wallet-transfer/internal/domain"
	"github.com/amantiwari/wallet-transfer/internal/testutil/memstore"
)

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

type sequenceIDs struct{ n int }

func (s *sequenceIDs) NewID() string {
	s.n++
	return "11111111-1111-4111-8111-" + leftPad(s.n)
}

func leftPad(n int) string {
	raw := "000000000000"
	value := ""
	for n > 0 {
		value = string(rune('0'+n%10)) + value
		n /= 10
	}
	if len(value) > 12 {
		return value
	}
	return raw[:12-len(value)] + value
}

type pingOK struct{}

func (pingOK) Ping(context.Context) error { return nil }

type pingFail struct{}

func (pingFail) Ping(context.Context) error { return context.DeadlineExceeded }

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := memstore.New()
	clock := fixedClock(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	log := slog.New(slog.DiscardHandler)
	wallets := wallet.NewService(store, store, clock, log)
	transfers := transfer.NewService(store, store.TransferReader(), store, clock, &sequenceIDs{}, log)
	handler := NewRouter(Dependencies{
		Wallets:        wallets,
		Transfers:      transfers,
		DB:             pingOK{},
		Logger:         log,
		RequestTimeout: 5 * time.Second,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestWalletAndTransferHTTPFlow(t *testing.T) {
	server := newTestServer(t)

	create := postJSON(t, server.URL+"/wallets", `{"id":"wallet_1","initialBalance":500}`)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create wallet status=%d body=%s", create.StatusCode, create.Body)
	}
	second := postJSON(t, server.URL+"/wallets", `{"id":"wallet_2","initialBalance":0}`)
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("second wallet status=%d body=%s", second.StatusCode, second.Body)
	}

	payload := `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
	first := postJSON(t, server.URL+"/transfers", payload)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("transfer status=%d body=%s", first.StatusCode, first.Body)
	}
	var created transferResponse
	decode(t, first.Body, &created)
	if created.Status != "PROCESSED" || created.Amount != 100 || created.ID == "" {
		t.Fatalf("transfer = %+v", created)
	}

	replay := postJSON(t, server.URL+"/transfers", payload)
	if replay.StatusCode != http.StatusCreated {
		t.Fatalf("replay status=%d body=%s", replay.StatusCode, replay.Body)
	}
	var again transferResponse
	decode(t, replay.Body, &again)
	if again.ID != created.ID || again.CreatedAt != created.CreatedAt {
		t.Fatalf("replay changed the transfer: %+v vs %+v", created, again)
	}

	from := getJSON(t, server.URL+"/wallets/wallet_1")
	var source walletResponse
	decode(t, from.Body, &source)
	if source.Balance != 400 {
		t.Fatalf("balance = %d", source.Balance)
	}

	ledger := getJSON(t, server.URL+"/transfers/"+created.ID+"/ledger")
	if ledger.StatusCode != http.StatusOK {
		t.Fatalf("ledger status=%d body=%s", ledger.StatusCode, ledger.Body)
	}
	var entries ledgerResponse
	decode(t, ledger.Body, &entries)
	if len(entries.Entries) != 2 || entries.Entries[0].Type != "DEBIT" || entries.Entries[1].Type != "CREDIT" {
		t.Fatalf("ledger = %+v", entries)
	}
	if entries.Entries[0].Amount != entries.Entries[1].Amount {
		t.Fatal("ledger does not balance")
	}
}

func TestTransferHTTPFailures(t *testing.T) {
	server := newTestServer(t)
	postJSON(t, server.URL+"/wallets", `{"id":"wallet_1","initialBalance":10}`)
	postJSON(t, server.URL+"/wallets", `{"id":"wallet_2","initialBalance":0}`)

	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{name: "float amount", body: `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10.5}`, status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{name: "negative amount", body: `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":-1}`, status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{name: "same wallet", body: `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_1","amount":1}`, status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{name: "missing key", body: `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1}`, status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{name: "unknown field", body: `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1,"note":"x"}`, status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{name: "trailing json", body: `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1}{}`, status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{name: "missing wallet", body: `{"idempotencyKey":"k1","fromWalletId":"wallet_9","toWalletId":"wallet_2","amount":1}`, status: http.StatusNotFound, code: "WALLET_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := postJSON(t, server.URL+"/transfers", tt.body)
			assertError(t, res, tt.status, tt.code)
		})
	}

	failed := postJSON(t, server.URL+"/transfers", `{"idempotencyKey":"poor","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":50}`)
	if failed.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", failed.StatusCode, failed.Body)
	}
	var transferBody transferResponse
	decode(t, failed.Body, &transferBody)
	if transferBody.Status != "FAILED" || transferBody.FailureReason != "INSUFFICIENT_FUNDS" {
		t.Fatalf("body = %+v", transferBody)
	}
	replay := postJSON(t, server.URL+"/transfers", `{"idempotencyKey":"poor","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":50}`)
	if replay.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(replay.Body, transferBody.ID) {
		t.Fatalf("replay = %d %s", replay.StatusCode, replay.Body)
	}

	conflict := postJSON(t, server.URL+"/transfers", `{"idempotencyKey":"poor","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1}`)
	assertError(t, conflict, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED")

	duplicateWallet := postJSON(t, server.URL+"/wallets", `{"id":"wallet_1","initialBalance":1}`)
	assertError(t, duplicateWallet, http.StatusConflict, "WALLET_EXISTS")

	missing := getJSON(t, server.URL+"/wallets/missing")
	assertError(t, missing, http.StatusNotFound, "WALLET_NOT_FOUND")

	text := postRaw(t, server.URL+"/transfers", "text/plain", "nope")
	assertError(t, text, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
}

func TestHealth(t *testing.T) {
	ok := httptest.NewServer(NewRouter(Dependencies{
		Wallets:        wallet.NewService(memstore.New(), memstore.New(), fixedClock(time.Now()), slog.New(slog.DiscardHandler)),
		Transfers:      &noopTransfers{},
		DB:             pingOK{},
		Logger:         slog.New(slog.DiscardHandler),
		RequestTimeout: time.Second,
	}))
	t.Cleanup(ok.Close)
	res := getJSON(t, ok.URL+"/health")
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Body, `"ok"`) {
		t.Fatalf("health = %d %s", res.StatusCode, res.Body)
	}

	bad := httptest.NewServer(NewRouter(Dependencies{
		Wallets:        wallet.NewService(memstore.New(), memstore.New(), fixedClock(time.Now()), slog.New(slog.DiscardHandler)),
		Transfers:      &noopTransfers{},
		DB:             pingFail{},
		Logger:         slog.New(slog.DiscardHandler),
		RequestTimeout: time.Second,
	}))
	t.Cleanup(bad.Close)
	down := getJSON(t, bad.URL+"/health")
	assertError(t, down, http.StatusServiceUnavailable, "UNAVAILABLE")
}

type noopTransfers struct{}

func (noopTransfers) Execute(context.Context, transfer.Command) (transfer.Result, error) {
	return transfer.Result{}, nil
}
func (noopTransfers) Get(context.Context, string) (domain.Transfer, error) {
	return domain.Transfer{}, &domain.NotFoundError{Entity: "transfer", ID: "missing"}
}
func (noopTransfers) Ledger(context.Context, string) ([]domain.LedgerEntry, error) {
	return nil, &domain.NotFoundError{Entity: "transfer", ID: "missing"}
}

type recorded struct {
	StatusCode int
	Body       string
	Header     http.Header
}

func postJSON(t *testing.T, url, body string) recorded {
	t.Helper()
	return postRaw(t, url, "application/json", body)
}

func postRaw(t *testing.T, url, contentType, body string) recorded {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Request-Id", "req-1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("X-Request-Id") == "" {
		t.Fatal("missing request id")
	}
	return recorded{StatusCode: res.StatusCode, Body: string(payload), Header: res.Header}
}

func getJSON(t *testing.T, url string) recorded {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return recorded{StatusCode: res.StatusCode, Body: string(payload), Header: res.Header}
}

func decode(t *testing.T, body string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), dst); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func assertError(t *testing.T, res recorded, status int, code string) {
	t.Helper()
	if res.StatusCode != status {
		t.Fatalf("status=%d body=%s", res.StatusCode, res.Body)
	}
	var payload errorBody
	decode(t, res.Body, &payload)
	if payload.Error.Code != code {
		t.Fatalf("code=%s body=%s", payload.Error.Code, res.Body)
	}
	if strings.Contains(strings.ToLower(res.Body), "pq:") || strings.Contains(res.Body, "SQLSTATE") {
		t.Fatalf("leaked database error: %s", res.Body)
	}
}
