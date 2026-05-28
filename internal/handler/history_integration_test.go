//go:build integration

package handler_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

// TestHTTP_GetTransferByID_HappyPath creates a transfer then
// fetches it by id, verifying the round-trip and the response
// shape.
func TestHTTP_GetTransferByID_HappyPath(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w1", "balance": 100}))
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w2", "balance": 0}))

	r := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "k", "fromWalletId": "w1", "toWalletId": "w2", "amount": 10,
	})
	mustOK(t, r)
	var created map[string]any
	_ = json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()

	resp, err := http.Get(srv.URL + "/transfers/" + created["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["id"] != created["id"] {
		t.Fatalf("id mismatch: %v vs %v", got["id"], created["id"])
	}
}

// TestHTTP_GetTransferByID_NotFound returns 404 for unknown ids.
func TestHTTP_GetTransferByID_NotFound(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	resp, err := http.Get(srv.URL + "/transfers/00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestHTTP_GetTransferByID_MalformedUUID returns 400, not 500 or 404.
// "not-a-uuid" should never make it to the DB layer.
func TestHTTP_GetTransferByID_MalformedUUID(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	resp, err := http.Get(srv.URL + "/transfers/not-a-uuid")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestHTTP_ListWalletTransfers_EmptyForUnknownWallet returns 200
// with an empty transfers array. We deliberately do NOT 404 on
// unknown wallets because it would leak the existence of arbitrary
// wallet ids.
func TestHTTP_ListWalletTransfers_EmptyForUnknownWallet(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	resp, err := http.Get(srv.URL + "/wallets/ghost/transfers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if len(body["transfers"].([]any)) != 0 {
		t.Fatalf("expected empty transfers: %v", body)
	}
}

// TestHTTP_ListWalletTransfers_HappyPathWithPagination exercises
// the cursor walk through the HTTP layer.
func TestHTTP_ListWalletTransfers_HappyPathWithPagination(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "src", "balance": 1000}))
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "dst", "balance": 0}))

	const N = 5
	for i := 0; i < N; i++ {
		time.Sleep(time.Millisecond)
		mustOK(t, postJSON(t, srv.URL+"/transfers", map[string]any{
			"idempotencyKey": "k-" + strconv.Itoa(i),
			"fromWalletId":   "src", "toWalletId": "dst", "amount": 10,
		}))
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		url := srv.URL + "/wallets/src/transfers?limit=2"
		if cursor != "" {
			url += "&before=" + cursor
		}
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Transfers  []map[string]any `json:"transfers"`
			NextCursor string           `json:"nextCursor"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()

		for _, tr := range body.Transfers {
			id := tr["id"].(string)
			if seen[id] {
				t.Fatalf("duplicate id across pages: %s", id)
			}
			seen[id] = true
		}
		if body.NextCursor == "" {
			break
		}
		cursor = body.NextCursor
	}
	if len(seen) != N {
		t.Fatalf("expected %d unique transfers across pagination, got %d", N, len(seen))
	}
}

// TestHTTP_ListWalletTransfers_BadLimit rejects non-numeric or
// negative limits with 400 rather than silently defaulting.
func TestHTTP_ListWalletTransfers_BadLimit(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	for _, q := range []string{"limit=abc", "limit=-3"} {
		t.Run(q, func(t *testing.T) {
			resp, err := http.Get(srv.URL + "/wallets/x/transfers?" + q)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status: %d", resp.StatusCode)
			}
		})
	}
}

// TestHTTP_ListWalletTransfers_BadBefore rejects malformed cursor
// timestamps with 400 rather than scanning all rows.
func TestHTTP_ListWalletTransfers_BadBefore(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	resp, err := http.Get(srv.URL + "/wallets/x/transfers?before=not-a-time")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestHTTP_ListWalletTransfers_LimitOneReturnsCursor: when limit=1
// and there are >= 2 results, the response must carry a nextCursor.
func TestHTTP_ListWalletTransfers_LimitOneReturnsCursor(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "src", "balance": 100}))
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "dst", "balance": 0}))
	for i := 0; i < 2; i++ {
		time.Sleep(time.Millisecond)
		mustOK(t, postJSON(t, srv.URL+"/transfers", map[string]any{
			"idempotencyKey": "k-" + strconv.Itoa(i),
			"fromWalletId":   "src", "toWalletId": "dst", "amount": 1,
		}))
	}
	resp, err := http.Get(srv.URL + "/wallets/src/transfers?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Transfers  []map[string]any `json:"transfers"`
		NextCursor string           `json:"nextCursor"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(body.Transfers))
	}
	if body.NextCursor == "" {
		t.Fatal("expected nextCursor when page is full")
	}
}

// TestHTTP_ListWalletTransfers_NoCursorOnLastPage: when limit
// exceeds available results, nextCursor should be omitted (the
// "you've reached the end" signal).
func TestHTTP_ListWalletTransfers_NoCursorOnLastPage(t *testing.T) {
	testdb.Reset(t)
	srv := newServer(t)
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "src", "balance": 100}))
	mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "dst", "balance": 0}))
	mustOK(t, postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "only-one", "fromWalletId": "src", "toWalletId": "dst", "amount": 1,
	}))
	resp, err := http.Get(srv.URL + "/wallets/src/transfers?limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Transfers  []map[string]any `json:"transfers"`
		NextCursor string           `json:"nextCursor"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.NextCursor != "" {
		t.Fatalf("expected no nextCursor on last page, got %q", body.NextCursor)
	}
}
