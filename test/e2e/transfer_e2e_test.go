// Package e2e exercises the full HTTP stack against the in-memory repository.
// No database required, so it runs in the default `go test ./...` and in CI.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	httphandler "github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/handler/http"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/platform/id"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/memory"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

func newServer(t *testing.T) (*httptest.Server, *memory.Store) {
	t.Helper()
	store := memory.New()
	store.SeedWallet("wallet_1", 1_000, "USD")
	store.SeedWallet("wallet_2", 0, "USD")
	repos := service.Repositories{Wallets: store, Transfers: store, Ledger: store, Idempotency: store}
	transferSvc := service.NewTransferService(repos, store, id.NewGenerator("txn", 12), nil)
	walletSvc := service.NewWalletService(store)

	srv := httptest.NewServer(httphandler.NewRouter(httphandler.Deps{
		Transfers: transferSvc,
		Wallets:   walletSvc,
	}))
	t.Cleanup(srv.Close)
	return srv, store
}

func postTransfer(t *testing.T, base string, payload map[string]any) *http.Response {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(base+"/transfers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /transfers: %v", err)
	}
	return resp
}

func TestE2E_TransferAndIdempotentReplay(t *testing.T) {
	srv, store := newServer(t)

	payload := map[string]any{
		"idempotencyKey": "e2e-1",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         400,
	}

	resp := postTransfer(t, srv.URL, payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first transfer status = %d, want 201", resp.StatusCode)
	}
	var first map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&first)
	resp.Body.Close()

	// Same key again -> 200 replay, no extra side effects.
	resp = postTransfer(t, srv.URL, payload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d, want 200", resp.StatusCode)
	}
	var second map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&second)
	resp.Body.Close()

	if first["id"] != second["id"] {
		t.Fatalf("replay returned different transfer: %v vs %v", first["id"], second["id"])
	}

	balResp, err := http.Get(srv.URL + "/wallets/wallet_1/balance")
	if err != nil {
		t.Fatalf("GET balance: %v", err)
	}
	var bal struct {
		Balance int64 `json:"balance"`
	}
	_ = json.NewDecoder(balResp.Body).Decode(&bal)
	balResp.Body.Close()
	if bal.Balance != 600 {
		t.Fatalf("wallet_1 balance = %d, want 600 (moved once)", bal.Balance)
	}
	if store.LedgerLen() != 2 {
		t.Fatalf("ledger entries = %d, want 2", store.LedgerLen())
	}
}

func TestE2E_ConcurrentDebitsNoDoubleSpend(t *testing.T) {
	srv, store := newServer(t)

	const workers = 40
	const amount = 100 // wallet_1 funds only 10

	var created, unprocessable atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			resp := postTransfer(t, srv.URL, map[string]any{
				"idempotencyKey": fmt.Sprintf("e2e-conc-%d", i),
				"fromWalletId":   "wallet_1",
				"toWalletId":     "wallet_2",
				"amount":         amount,
			})
			resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusCreated:
				created.Add(1)
			case http.StatusUnprocessableEntity:
				unprocessable.Add(1)
			default:
				t.Errorf("worker %d: unexpected status %d", i, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()

	if created.Load() != 10 {
		t.Fatalf("created = %d, want 10", created.Load())
	}
	if unprocessable.Load() != workers-10 {
		t.Fatalf("unprocessable = %d, want %d", unprocessable.Load(), workers-10)
	}

	from, _ := store.Get(t.Context(), "wallet_1")
	to, _ := store.Get(t.Context(), "wallet_2")
	if from.Balance.Amount != 0 || to.Balance.Amount != 1_000 {
		t.Fatalf("final balances = %d / %d, want 0 / 1000", from.Balance.Amount, to.Balance.Amount)
	}
}
