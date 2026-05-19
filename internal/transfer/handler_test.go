package transfer_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hariharandr/wallet-transfer-assignment/internal/httpapi"
	"github.com/hariharandr/wallet-transfer-assignment/internal/transfer"
	"github.com/hariharandr/wallet-transfer-assignment/testsupport"
)

func newTestServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	pool := testsupport.StartPostgres(t)
	h := transfer.NewHandler(transfer.NewService(transfer.NewPgxRepository(pool)))
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.Routes{Transfer: h.Routes}))
	t.Cleanup(srv.Close)
	return srv, pool
}

func post(t *testing.T, base, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(base+"/transfers", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestHandler_CreateTransfer(t *testing.T) {
	srv, pool := newTestServer(t)

	t.Run("bad json is 400", func(t *testing.T) {
		testsupport.Reset(t, pool)
		resp := post(t, srv.URL, `{not json`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("happy path is 201", func(t *testing.T) {
		testsupport.Reset(t, pool)
		resp := post(t, srv.URL, `{"idempotencyKey":"h1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1000}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", resp.StatusCode)
		}
		var b map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&b)
		if b["status"] != "PROCESSED" || b["transferId"] == "" {
			t.Fatalf("unexpected body %v", b)
		}
	})

	t.Run("insufficient funds is 422 with failed transfer", func(t *testing.T) {
		testsupport.Reset(t, pool)
		resp := post(t, srv.URL, `{"idempotencyKey":"h2","fromWalletId":"wallet_3","toWalletId":"wallet_1","amount":100}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", resp.StatusCode)
		}
		var b map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&b)
		if b["error"] != "insufficient funds" || b["status"] != "FAILED" {
			t.Fatalf("unexpected body %v", b)
		}
	})

	t.Run("unknown wallet is 404", func(t *testing.T) {
		testsupport.Reset(t, pool)
		resp := post(t, srv.URL, `{"idempotencyKey":"h3","fromWalletId":"ghost","toWalletId":"wallet_1","amount":100}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}
