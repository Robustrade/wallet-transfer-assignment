package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/dbtest"
	"github.com/Robustrade/wallet-transfer-assignment/internal/httpapi"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type client struct {
	t      *testing.T
	server http.Handler
}

func newClient(t *testing.T, db *database.DB) *client {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &client{t: t, server: httpapi.NewServer(service.New(repository.NewStore(db)), logger)}
}

func (c *client) do(method, path, body string) (*http.Response, map[string]any) {
	c.t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c.server.ServeHTTP(rec, req)

	res := rec.Result()
	var decoded map[string]any
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil && err != io.EOF {
		c.t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return res, decoded
}

func (c *client) createWallet(id string, balance int64) {
	c.t.Helper()

	payload, err := json.Marshal(map[string]any{"id": id, "balance": balance})
	if err != nil {
		c.t.Fatalf("marshal wallet: %v", err)
	}
	if res, body := c.do(http.MethodPost, "/wallets", string(payload)); res.StatusCode != http.StatusCreated {
		c.t.Fatalf("create wallet %s: status %d, body %v", id, res.StatusCode, body)
	}
}

func transferBody(key, from, to string, amount int64) string {
	payload, _ := json.Marshal(map[string]any{
		"idempotencyKey": key, "fromWalletId": from, "toWalletId": to, "amount": amount,
	})
	return string(payload)
}

func errorCode(t *testing.T, body map[string]any) string {
	t.Helper()

	detail, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no error object: %v", body)
	}
	code, _ := detail["code"].(string)
	return code
}

func TestCreateTransferEndpoint(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		c := newClient(t, db)
		c.createWallet("alice", 500)
		c.createWallet("bob", 0)

		res, body := c.do(http.MethodPost, "/transfers", transferBody("key-1", "alice", "bob", 300))
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201: %v", res.StatusCode, body)
		}
		if got := body["status"]; got != "PROCESSED" {
			t.Errorf("status = %v, want PROCESSED", got)
		}
		if got := body["amount"]; got != float64(300) {
			t.Errorf("amount = %v, want 300", got)
		}
		entries, _ := body["entries"].([]any)
		if len(entries) != 2 {
			t.Errorf("entries = %d, want 2", len(entries))
		}
		if replay := res.Header.Get("Idempotent-Replay"); replay != "" {
			t.Errorf("Idempotent-Replay = %q on a first request, want empty", replay)
		}

		walletRes, wallet := c.do(http.MethodGet, "/wallets/alice", "")
		if walletRes.StatusCode != http.StatusOK {
			t.Fatalf("get wallet status = %d", walletRes.StatusCode)
		}
		if got := wallet["balance"]; got != float64(200) {
			t.Errorf("alice balance = %v, want 200", got)
		}

		transferID, _ := body["id"].(string)
		getRes, fetched := c.do(http.MethodGet, "/transfers/"+transferID, "")
		if getRes.StatusCode != http.StatusOK {
			t.Fatalf("get transfer status = %d", getRes.StatusCode)
		}
		if fetched["id"] != transferID {
			t.Errorf("fetched transfer id = %v, want %s", fetched["id"], transferID)
		}
	})
}

func TestReplayedRequestReturnsTheOriginalResponse(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		c := newClient(t, db)
		c.createWallet("alice", 500)
		c.createWallet("bob", 0)

		firstRes, first := c.do(http.MethodPost, "/transfers", transferBody("key-1", "alice", "bob", 300))
		secondRes, second := c.do(http.MethodPost, "/transfers", transferBody("key-1", "alice", "bob", 300))

		if secondRes.StatusCode != firstRes.StatusCode {
			t.Errorf("replay status = %d, want %d", secondRes.StatusCode, firstRes.StatusCode)
		}
		if secondRes.Header.Get("Idempotent-Replay") != "true" {
			t.Error("replay is missing the Idempotent-Replay header")
		}

		firstJSON, _ := json.Marshal(first)
		secondJSON, _ := json.Marshal(second)
		if !bytes.Equal(firstJSON, secondJSON) {
			t.Errorf("replay body\n%s\ndiffers from the original\n%s", secondJSON, firstJSON)
		}

		_, wallet := c.do(http.MethodGet, "/wallets/alice", "")
		if got := wallet["balance"]; got != float64(200) {
			t.Errorf("alice balance = %v, want 200: the replay moved money again", got)
		}
	})
}

func TestTransferEndpointErrors(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		c := newClient(t, db)
		c.createWallet("alice", 100)
		c.createWallet("bob", 0)

		if res, _ := c.do(http.MethodPost, "/transfers", transferBody("used-key", "alice", "bob", 10)); res.StatusCode != http.StatusCreated {
			t.Fatalf("seed transfer status = %d", res.StatusCode)
		}

		cases := []struct {
			name       string
			body       string
			wantStatus int
			wantCode   string
		}{
			{"unknown source wallet", transferBody("k1", "ghost", "bob", 10), http.StatusNotFound, "WALLET_NOT_FOUND"},
			{"unknown destination wallet", transferBody("k2", "alice", "ghost", 10), http.StatusNotFound, "WALLET_NOT_FOUND"},
			{"zero amount", transferBody("k3", "alice", "bob", 0), http.StatusBadRequest, "INVALID_REQUEST"},
			{"negative amount", transferBody("k4", "alice", "bob", -5), http.StatusBadRequest, "INVALID_REQUEST"},
			{"same wallet", transferBody("k5", "alice", "alice", 10), http.StatusBadRequest, "INVALID_REQUEST"},
			{"missing idempotency key", transferBody("", "alice", "bob", 10), http.StatusBadRequest, "INVALID_REQUEST"},
			{"insufficient funds", transferBody("k6", "alice", "bob", 5000), http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS"},
			{"key reused for a different transfer", transferBody("used-key", "alice", "bob", 99), http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT"},
			{"malformed json", "{not json", http.StatusBadRequest, "INVALID_REQUEST"},
			{"fractional amount", `{"idempotencyKey":"k7","fromWalletId":"alice","toWalletId":"bob","amount":10.5}`, http.StatusBadRequest, "INVALID_REQUEST"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				res, body := c.do(http.MethodPost, "/transfers", tc.body)
				if res.StatusCode != tc.wantStatus {
					t.Fatalf("status = %d, want %d: %v", res.StatusCode, tc.wantStatus, body)
				}
				if got := errorCode(t, body); got != tc.wantCode {
					t.Errorf("code = %q, want %q", got, tc.wantCode)
				}
			})
		}

		t.Run("unknown transfer", func(t *testing.T) {
			res, body := c.do(http.MethodGet, "/transfers/tr_missing", "")
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", res.StatusCode)
			}
			if got := errorCode(t, body); got != "TRANSFER_NOT_FOUND" {
				t.Errorf("code = %q, want TRANSFER_NOT_FOUND", got)
			}
		})

		t.Run("duplicate wallet", func(t *testing.T) {
			res, body := c.do(http.MethodPost, "/wallets", `{"id":"alice","balance":0}`)
			if res.StatusCode != http.StatusConflict {
				t.Fatalf("status = %d, want 409", res.StatusCode)
			}
			if got := errorCode(t, body); got != "WALLET_ALREADY_EXISTS" {
				t.Errorf("code = %q, want WALLET_ALREADY_EXISTS", got)
			}
		})

		t.Run("negative opening balance", func(t *testing.T) {
			res, body := c.do(http.MethodPost, "/wallets", `{"id":"dave","balance":-1}`)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", res.StatusCode)
			}
			if got := errorCode(t, body); got != "INVALID_REQUEST" {
				t.Errorf("code = %q, want INVALID_REQUEST", got)
			}
		})

		t.Run("unknown wallet", func(t *testing.T) {
			res, body := c.do(http.MethodGet, "/wallets/ghost", "")
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", res.StatusCode)
			}
			if got := errorCode(t, body); got != "WALLET_NOT_FOUND" {
				t.Errorf("code = %q, want WALLET_NOT_FOUND", got)
			}
		})
	})
}
func TestDatabaseFailuresAreNotLeaked(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		c := newClient(t, db)
		c.createWallet("alice", 100)
		c.createWallet("bob", 0)

		if err := db.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}

		res, body := c.do(http.MethodPost, "/transfers", transferBody("key-1", "alice", "bob", 10))
		if res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500: %v", res.StatusCode, body)
		}
		if got := errorCode(t, body); got != "INTERNAL_ERROR" {
			t.Errorf("code = %q, want INTERNAL_ERROR", got)
		}

		detail, _ := body["error"].(map[string]any)
		message, _ := detail["message"].(string)
		for _, leak := range []string{"sql", "SQLSTATE", "pgx", "sqlite", "wallets"} {
			if strings.Contains(strings.ToLower(message), strings.ToLower(leak)) {
				t.Errorf("error message %q leaks internal detail %q", message, leak)
			}
		}
	})
}
