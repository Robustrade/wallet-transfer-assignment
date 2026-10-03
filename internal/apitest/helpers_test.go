package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// beginTest runs t in parallel with other tests and checks the ledger
// invariants once t ends.
func beginTest(t *testing.T) {
	t.Helper()
	t.Parallel()
	t.Cleanup(func() { checkInvariants(t) })
}

// createWallet stores a wallet with a unique ID and the given opening
// balance, and returns its ID.
func createWallet(t *testing.T, balance int64) string {
	t.Helper()
	id := "wallet-" + uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, opening_balance, balance) VALUES ($1, $2, $2)`, id, balance)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	return id
}

// newKey returns an idempotency key that no other test uses.
func newKey() string {
	return "key-" + uuid.NewString()
}

// transferBody returns a valid POST /transfers body.
func transferBody(key, from, to string, amount int64) string {
	body, _ := json.Marshal(map[string]any{
		"idempotencyKey": key,
		"fromWalletId":   from,
		"toWalletId":     to,
		"amount":         amount,
	})
	return string(body)
}

// apiResponse is a response from POST /transfers.
type apiResponse struct {
	status int
	body   []byte
}

// send posts body to the server at baseURL.
func send(baseURL, body string) (apiResponse, error) {
	resp, err := http.Post(baseURL+"/transfers", "application/json", strings.NewReader(body))
	if err != nil {
		return apiResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiResponse{}, err
	}
	return apiResponse{status: resp.StatusCode, body: data}, nil
}

func postTransfer(t *testing.T, body string) apiResponse {
	t.Helper()
	return postTransferTo(t, apiURL, body)
}

func postTransferTo(t *testing.T, baseURL, body string) apiResponse {
	t.Helper()
	resp, err := send(baseURL, body)
	if err != nil {
		t.Fatalf("POST /transfers: %v", err)
	}
	return resp
}

// postConcurrently releases all bodies at the same moment and returns the
// responses in the same order.
func postConcurrently(t *testing.T, bodies []string) []apiResponse {
	t.Helper()
	responses := make([]apiResponse, len(bodies))
	errs := make([]error, len(bodies))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, body := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			responses[i], errs[i] = send(apiURL, body)
		}()
	}
	close(start)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("POST /transfers: %v", err)
	}
	return responses
}

// startServer starts another API server with its own pool on dbURL, logging
// to logger, and returns the server's URL.
func startServer(t *testing.T, dbURL string, logger *slog.Logger) string {
	t.Helper()
	serverPool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(serverPool.Close)
	server := httptest.NewServer(newHandler(serverPool, logger))
	t.Cleanup(server.Close)
	return server.URL
}

// assertSameResponse fails t unless got has the same status and body bytes
// as the original response.
func assertSameResponse(t *testing.T, got, original apiResponse) {
	t.Helper()
	if got.status != original.status || !bytes.Equal(got.body, original.body) {
		t.Errorf("response = %d %s, want the original %d %s", got.status, got.body, original.status, original.body)
	}
}

// transferResponse is the transfer body from spec §3.
type transferResponse struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
}

// transfer checks the status and decodes a transfer body, failing on any
// field the spec does not list.
func (r apiResponse) transfer(t *testing.T, wantStatus int) transferResponse {
	t.Helper()
	if r.status != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", r.status, wantStatus, r.body)
	}
	var got transferResponse
	dec := json.NewDecoder(bytes.NewReader(r.body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode transfer body %s: %v", r.body, err)
	}
	return got
}

// errorResponse is the error body from spec §4.
type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// errorBody checks the status and decodes an error body, failing on any field
// the spec does not list or on an empty message.
func (r apiResponse) errorBody(t *testing.T, wantStatus int) errorResponse {
	t.Helper()
	if r.status != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", r.status, wantStatus, r.body)
	}
	var got errorResponse
	dec := json.NewDecoder(bytes.NewReader(r.body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode error body %s: %v", r.body, err)
	}
	if got.Error.Message == "" {
		t.Errorf("error message is empty; body: %s", r.body)
	}
	return got
}

// transferCount returns how many stored transfers involve a wallet.
func transferCount(t *testing.T, walletID string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM transfers WHERE from_wallet_id = $1 OR to_wallet_id = $1`,
		walletID).Scan(&count)
	if err != nil {
		t.Fatalf("count transfers of %s: %v", walletID, err)
	}
	return count
}

// assertUntouched fails t if any transfer involves the wallet or its balance
// is not wantBalance.
func assertUntouched(t *testing.T, walletID string, wantBalance int64) {
	t.Helper()
	if n := transferCount(t, walletID); n != 0 {
		t.Errorf("%d transfers stored for %s, want none", n, walletID)
	}
	if balance := balanceOf(t, walletID); balance != wantBalance {
		t.Errorf("balance of %s = %d, want %d", walletID, balance, wantBalance)
	}
}

func balanceOf(t *testing.T, walletID string) int64 {
	t.Helper()
	var balance int64
	err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&balance)
	if err != nil {
		t.Fatalf("read balance of %s: %v", walletID, err)
	}
	return balance
}

// storedTransfer is a row of the transfers table.
type storedTransfer struct {
	key, from, to string
	amount        int64
	state         string
}

func loadTransfer(t *testing.T, id string) storedTransfer {
	t.Helper()
	var s storedTransfer
	err := pool.QueryRow(context.Background(), `
		SELECT idempotency_key, from_wallet_id, to_wallet_id, amount, state::text
		FROM transfers WHERE id = $1`, id).Scan(&s.key, &s.from, &s.to, &s.amount, &s.state)
	if err != nil {
		t.Fatalf("load transfer %s: %v", id, err)
	}
	return s
}

// ledgerEntry is a row of the ledger_entries table.
type ledgerEntry struct {
	walletID, entryType string
	amount              int64
}

// ledgerEntriesOf returns a transfer's entries, DEBIT first (the enum order).
func ledgerEntriesOf(t *testing.T, transferID string) []ledgerEntry {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT wallet_id, type::text AS entry_type, amount
		FROM ledger_entries WHERE transfer_id = $1 ORDER BY type`, transferID)
	if err != nil {
		t.Fatalf("query ledger entries of %s: %v", transferID, err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledgerEntry, error) {
		var e ledgerEntry
		err := row.Scan(&e.walletID, &e.entryType, &e.amount)
		return e, err
	})
	if err != nil {
		t.Fatalf("read ledger entries of %s: %v", transferID, err)
	}
	return entries
}

// invariantChecks hold one query per design §8 invariant that the schema
// cannot enforce. Each query returns the rows that break it. Invariant 8
// (only PENDING -> PROCESSED or FAILED) follows from 7 and the domain's
// guarded transitions.
var invariantChecks = []struct{ invariant, query string }{
	{
		// UNIQUE (transfer_id, type) allows at most two entries, so two
		// matching ones mean exactly these two.
		"5: a PROCESSED transfer has a DEBIT on the source and a CREDIT on the destination, both for its amount",
		`SELECT t.id::text FROM transfers t
		 WHERE t.state = 'PROCESSED'
		   AND (SELECT count(*) FROM ledger_entries e
		        WHERE e.transfer_id = t.id AND e.amount = t.amount
		          AND ((e.type = 'DEBIT' AND e.wallet_id = t.from_wallet_id)
		            OR (e.type = 'CREDIT' AND e.wallet_id = t.to_wallet_id))) <> 2`,
	},
	{
		"6: a FAILED transfer has no ledger entries",
		`SELECT t.id::text FROM transfers t
		 WHERE t.state = 'FAILED'
		   AND EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id)`,
	},
	{
		"7: no transfer is committed as PENDING",
		`SELECT id::text FROM transfers WHERE state = 'PENDING'`,
	},
	{
		"9: total DEBIT equals total CREDIT",
		`SELECT 'DEBIT ' || COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'), 0)
		     || ', CREDIT ' || COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0)
		 FROM ledger_entries
		 HAVING COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'), 0)
		     <> COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0)`,
	},
	{
		"10: every balance equals opening_balance + credits - debits",
		`SELECT w.id FROM wallets w
		 LEFT JOIN (SELECT wallet_id,
		                   SUM(CASE type WHEN 'CREDIT' THEN amount ELSE -amount END) AS net
		            FROM ledger_entries GROUP BY wallet_id) e ON e.wallet_id = w.id
		 WHERE w.balance <> w.opening_balance + COALESCE(e.net, 0)`,
	},
}

// checkInvariants fails t if any committed data breaks an invariant. It
// checks the whole database: every committed transaction must leave the
// invariants true, whichever test made it.
func checkInvariants(t *testing.T) {
	t.Helper()
	for _, c := range invariantChecks {
		rows, err := pool.Query(context.Background(), c.query)
		if err != nil {
			t.Errorf("invariant %s: query failed: %v", c.invariant, err)
			continue
		}
		broken, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Errorf("invariant %s: read failed: %v", c.invariant, err)
			continue
		}
		if len(broken) > 0 {
			t.Errorf("invariant %s is broken by %v", c.invariant, broken)
		}
	}
}
