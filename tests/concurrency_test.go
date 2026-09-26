package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"wallet/internal/constants"
	"wallet/internal/dto"
	"wallet/internal/service"
	"wallet/tests/testutil"
)

// This file races requests against the real PostgreSQL database through the
// full HTTP stack. Nothing here is mocked: the properties under test (row
// locking, unique indexes, transaction isolation) only exist in the database.

// racingClient is the HTTP client used for racing requests. The timeout is
// generous by design: a racing request should surface the server's status,
// not a client-side deadline.
var racingClient = &http.Client{Timeout: 30 * time.Second}

// raceResult is one racing request's outcome.
type raceResult struct {
	i      int
	status int
	body   []byte
	err    error
}

// race fires n concurrent POST /transfers requests, one per goroutine, with
// bodies from makeBody(i). Every goroutine blocks on a shared start channel
// that is closed only after all requests are prepared, so they hit the server
// as close to simultaneously as the platform allows.
//
// Goroutines never call t.Fatal (it must only run on the test goroutine);
// transport failures are recorded in the result and asserted afterwards.
func race(t *testing.T, srv *httptest.Server, n int, makeBody func(i int) string) []raceResult {
	t.Helper()

	results := make([]raceResult, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := makeBody(i)
			<-start
			resp, err := racingClient.Post(srv.URL+"/transfers", "application/json", strings.NewReader(body))
			if err != nil {
				results[i] = raceResult{i: i, err: err}
				return
			}
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				results[i] = raceResult{i: i, err: fmt.Errorf("read response: %w", err)}
				return
			}
			results[i] = raceResult{i: i, status: resp.StatusCode, body: raw}
		}(i)
	}
	close(start)
	wg.Wait()
	return results
}

// transferBody renders a POST /transfers body; %q keeps ids and keys escaped.
func transferBody(key, from, to string, amount int64) string {
	return fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":%d}`,
		key, from, to, amount)
}

// requireNoTransportErrors fails the test if any racing request did not
// receive an HTTP response at all.
func requireNoTransportErrors(t *testing.T, results []raceResult) {
	t.Helper()
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("request %d did not get an HTTP response: %v", r.i, r.err)
		}
	}
}

// statusCounts buckets results by HTTP status.
func statusCounts(results []raceResult) map[int]int {
	counts := make(map[int]int, 4)
	for _, r := range results {
		counts[r.status]++
	}
	return counts
}

// describeFailures renders every result whose status is not want, for use in
// failure messages.
func describeFailures(results []raceResult, want int) string {
	var b strings.Builder
	for _, r := range results {
		if r.status == want {
			continue
		}
		if r.err != nil {
			fmt.Fprintf(&b, "\n  request %d: %v", r.i, r.err)
			continue
		}
		fmt.Fprintf(&b, "\n  request %d: status %d body %s", r.i, r.status, r.body)
	}
	return b.String()
}

// bodiesWithStatus returns, in request order, the bodies of results carrying
// the given status.
func bodiesWithStatus(results []raceResult, status int) [][]byte {
	var bodies [][]byte
	for _, r := range results {
		if r.status == status {
			bodies = append(bodies, r.body)
		}
	}
	return bodies
}

// requireIdenticalBodies fails unless every body is byte-identical, and
// returns the shared body.
func requireIdenticalBodies(t *testing.T, bodies [][]byte) []byte {
	t.Helper()
	if len(bodies) == 0 {
		t.Fatal("no bodies to compare")
	}
	first := bodies[0]
	for i, b := range bodies[1:] {
		if !bytes.Equal(b, first) {
			t.Errorf("body %d differs from the first:\n first: %s\n  this: %s", i+1, first, b)
		}
	}
	return first
}

// decodeTransfer decodes a 201 response body.
func decodeTransfer(t *testing.T, raw []byte) dto.TransferResponse {
	t.Helper()
	var resp dto.TransferResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode transfer response %s: %v", raw, err)
	}
	return resp
}

// decodeError decodes an error response body.
func decodeError(t *testing.T, raw []byte) dto.ErrorResponse {
	t.Helper()
	var resp dto.ErrorResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode error response %s: %v", raw, err)
	}
	return resp
}

// requireUniqueTransferIDs asserts that the 201 responses carry n distinct
// transfer ids: each concurrent request created its own transfer, and no
// response duplicates another's.
func requireUniqueTransferIDs(t *testing.T, results []raceResult, n int) {
	t.Helper()
	seen := make(map[string]bool, n)
	for _, r := range results {
		if r.status != http.StatusCreated {
			continue
		}
		tr := decodeTransfer(t, r.body)
		if tr.TransferID == "" {
			t.Errorf("request %d: 201 body without a transferId: %s", r.i, r.body)
			continue
		}
		if seen[tr.TransferID] {
			t.Errorf("transfer %s appears in more than one 201 response", tr.TransferID)
		}
		seen[tr.TransferID] = true
	}
	if len(seen) != n {
		t.Errorf("distinct transfer ids = %d, want %d", len(seen), n)
	}
}

// countTransfers counts transfers in the given state, straight from the
// database.
func countTransfers(t *testing.T, env *testutil.Env, status string) int64 {
	t.Helper()
	return env.CountRows(t, "SELECT count(*) FROM transfers WHERE status = $1", status)
}

// sumBalances is the conservation-of-money probe: transfers move money, they
// never create or destroy it, so the sum over all wallets must not change.
// (Some transfers here are in flight in the transactional sense - every
// balance change is committed atomically with its transfer - so a stable sum
// also means no test left a half-applied transfer behind.)
func sumBalances(t *testing.T, env *testutil.Env) int64 {
	t.Helper()
	return env.CountRows(t, "SELECT coalesce(sum(balance), 0) FROM wallets")
}

// ledgerFor returns the row count and amount total of one wallet's entries of
// one type (DEBIT or CREDIT).
func ledgerFor(t *testing.T, env *testutil.Env, walletID, entryType string) (count, sum int64) {
	t.Helper()
	return env.CountRows(t, "SELECT count(*) FROM ledger_entries WHERE wallet_id = $1 AND entry_type = $2", walletID, entryType),
		env.CountRows(t, "SELECT coalesce(sum(amount), 0) FROM ledger_entries WHERE wallet_id = $1 AND entry_type = $2", walletID, entryType)
}

// requireLedgerWellFormed asserts the ledger invariants that must hold after
// any mix of committed transfers:
//
//   - exactly wantProcessed transfers are PROCESSED, and exactly two ledger
//     rows exist per processed transfer;
//   - every DEBIT is pinned to its transfer's source wallet, every CREDIT to
//     the destination wallet, both equal to the transfer's amount (the
//     composite foreign key enforces the amount structurally);
//   - no ledger row exists for a FAILED transfer;
//   - each processed transfer has entries, and none of its rows belongs to
//     another transfer;
//   - the debit total equals the credit total (double-entry conservation).
func requireLedgerWellFormed(t *testing.T, env *testutil.Env, wantProcessed int64) {
	t.Helper()

	if got := countTransfers(t, env, string(constants.TransferProcessed)); got != wantProcessed {
		t.Errorf("PROCESSED transfers = %d, want %d", got, wantProcessed)
	}
	debits := env.CountRows(t, `SELECT count(*)
		FROM ledger_entries e
		JOIN transfers t ON t.id = e.transfer_id
		WHERE e.entry_type = 'DEBIT' AND e.wallet_id = t.from_wallet_id AND e.amount = t.amount`)
	if debits != wantProcessed {
		t.Errorf("DEBIT entries pinned to their transfer's source wallet = %d, want %d", debits, wantProcessed)
	}
	credits := env.CountRows(t, `SELECT count(*)
		FROM ledger_entries e
		JOIN transfers t ON t.id = e.transfer_id
		WHERE e.entry_type = 'CREDIT' AND e.wallet_id = t.to_wallet_id AND e.amount = t.amount`)
	if credits != wantProcessed {
		t.Errorf("CREDIT entries pinned to their transfer's destination wallet = %d, want %d", credits, wantProcessed)
	}
	if got := env.CountRows(t, `SELECT count(*)
		FROM ledger_entries e
		JOIN transfers t ON t.id = e.transfer_id
		WHERE t.status = 'FAILED'`); got != 0 {
		t.Errorf("ledger entries attached to FAILED transfers = %d, want 0", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); got != 2*wantProcessed {
		t.Errorf("ledger entries = %d, want %d", got, 2*wantProcessed)
	}
	if got := env.CountRows(t, "SELECT count(DISTINCT transfer_id) FROM ledger_entries"); got != wantProcessed {
		t.Errorf("transfers with ledger entries = %d, want %d", got, wantProcessed)
	}
	debitSum := env.CountRows(t, "SELECT coalesce(sum(amount), 0) FROM ledger_entries WHERE entry_type = 'DEBIT'")
	creditSum := env.CountRows(t, "SELECT coalesce(sum(amount), 0) FROM ledger_entries WHERE entry_type = 'CREDIT'")
	if debitSum != creditSum {
		t.Errorf("debit total %d != credit total %d", debitSum, creditSum)
	}
}

// TestConcurrentTransfersFromSameWalletWithinBalance proves that concurrent
// transfers drawing on one wallet all commit when the balance covers them:
// the row lock serializes the debits, every request succeeds, and the ledger
// records each of them exactly once.
func TestConcurrentTransfersFromSameWalletWithinBalance(t *testing.T) {
	const racers = 8

	env := testutil.NewEnv(t)
	env.CreateWallet(t, "src_within", 1000)
	for i := 0; i < racers; i++ {
		env.CreateWallet(t, fmt.Sprintf("dst_within_%d", i), 0)
	}
	srv := newServer(t, env)

	results := race(t, srv, racers, func(i int) string {
		return transferBody(fmt.Sprintf("race-within-%d", i), "src_within", fmt.Sprintf("dst_within_%d", i), 100)
	})
	requireNoTransportErrors(t, results)

	if got := statusCounts(results)[http.StatusCreated]; got != racers {
		t.Fatalf("201 responses = %d, want %d;%s", got, racers, describeFailures(results, http.StatusCreated))
	}
	requireUniqueTransferIDs(t, results, racers)

	if got := env.Balance(t, "src_within"); got != 200 {
		t.Errorf("src_within balance = %d, want 200 (8 x 100 debited)", got)
	}
	for i := 0; i < racers; i++ {
		dst := fmt.Sprintf("dst_within_%d", i)
		if got := env.Balance(t, dst); got != 100 {
			t.Errorf("%s balance = %d, want 100", dst, got)
		}
		if n, sum := ledgerFor(t, env, dst, "CREDIT"); n != 1 || sum != 100 {
			t.Errorf("%s CREDIT entries = %d totaling %d, want 1 totaling 100", dst, n, sum)
		}
	}
	if n, sum := ledgerFor(t, env, "src_within", "DEBIT"); n != racers || sum != 800 {
		t.Errorf("src_within DEBIT entries = %d totaling %d, want %d totaling 800", n, sum, racers)
	}

	if got := countTransfers(t, env, string(constants.TransferFailed)); got != 0 {
		t.Errorf("FAILED transfers = %d, want 0", got)
	}
	requireLedgerWellFormed(t, env, racers)
	if got := sumBalances(t, env); got != 1000 {
		t.Errorf("sum of balances = %d, want 1000 (conservation of money)", got)
	}
}

// TestConcurrentTransfersFromSameWalletOverBalance proves the guard clause
// decides sufficiency on the locked row, not on stale reads: six concurrent
// requests of 100 against a balance of 500 produce exactly five successes and
// one INSUFFICIENT_FUNDS failure, no matter how they interleave. The failure
// is itself a committed, replayable outcome - a FAILED transfer with no
// ledger entries - so a rejected request is auditable without having moved
// money.
func TestConcurrentTransfersFromSameWalletOverBalance(t *testing.T) {
	const racers = 6

	env := testutil.NewEnv(t)
	env.CreateWallet(t, "src_over", 500)
	for i := 0; i < racers; i++ {
		env.CreateWallet(t, fmt.Sprintf("dst_over_%d", i), 0)
	}
	srv := newServer(t, env)

	results := race(t, srv, racers, func(i int) string {
		return transferBody(fmt.Sprintf("race-over-%d", i), "src_over", fmt.Sprintf("dst_over_%d", i), 100)
	})
	requireNoTransportErrors(t, results)

	counts := statusCounts(results)
	if counts[http.StatusCreated] != 5 || counts[http.StatusUnprocessableEntity] != 1 || len(counts) != 2 {
		t.Fatalf("status counts = %v, want exactly 5x201 and 1x422", counts)
	}
	requireUniqueTransferIDs(t, results, 5)

	// The single losing response is the recorded INSUFFICIENT_FUNDS error and
	// points at a persisted FAILED transfer.
	loser := decodeError(t, requireIdenticalBodies(t, bodiesWithStatus(results, http.StatusUnprocessableEntity)))
	if loser.Error.Code != constants.CodeInsufficientFunds {
		t.Errorf("422 error code = %q, want %q", loser.Error.Code, constants.CodeInsufficientFunds)
	}
	if loser.Error.TransferID == "" {
		t.Error("422 body must carry the failed transfer's id")
	}
	if got := env.CountRows(t, `SELECT count(*)
		FROM transfers
		WHERE id = $1::uuid AND status = 'FAILED' AND failure_reason = $2`,
		loser.Error.TransferID, constants.FailureInsufficientFunds); got != 1 {
		t.Errorf("FAILED transfer %s with reason %s: count = %d, want 1",
			loser.Error.TransferID, constants.FailureInsufficientFunds, got)
	}

	if got := env.Balance(t, "src_over"); got != 0 {
		t.Errorf("src_over balance = %d, want 0 (all 500 moved out)", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM wallets WHERE id LIKE 'dst_over_%' AND balance = 100"); got != 5 {
		t.Errorf("destinations credited with 100 = %d, want 5", got)
	}
	if n, sum := ledgerFor(t, env, "src_over", "DEBIT"); n != 5 || sum != 500 {
		t.Errorf("src_over DEBIT entries = %d totaling %d, want 5 totaling 500", n, sum)
	}

	if got := countTransfers(t, env, string(constants.TransferFailed)); got != 1 {
		t.Errorf("FAILED transfers = %d, want 1", got)
	}
	requireLedgerWellFormed(t, env, 5)
	if got := sumBalances(t, env); got != 500 {
		t.Errorf("sum of balances = %d, want 500 (conservation of money)", got)
	}
}

// TestManyConcurrentTransfersFromOneWallet is the saturation case: 50 racing
// transfers from a single wallet whose balance covers exactly all of them.
// It proves the serialized lock waits stay within lock_timeout under real
// contention, that the last debit lands on a zero balance, and that a burst
// of commits leaves a perfectly formed ledger rather than lost or duplicated
// entries.
func TestManyConcurrentTransfersFromOneWallet(t *testing.T) {
	const racers = 50

	env := testutil.NewEnv(t)
	env.CreateWallet(t, "src_many", racers*100)
	for i := 0; i < racers; i++ {
		env.CreateWallet(t, fmt.Sprintf("dst_many_%d", i), 0)
	}
	srv := newServer(t, env)

	results := race(t, srv, racers, func(i int) string {
		return transferBody(fmt.Sprintf("race-many-%d", i), "src_many", fmt.Sprintf("dst_many_%d", i), 100)
	})
	requireNoTransportErrors(t, results)

	if got := statusCounts(results)[http.StatusCreated]; got != racers {
		t.Fatalf("201 responses = %d, want %d;%s", got, racers, describeFailures(results, http.StatusCreated))
	}
	requireUniqueTransferIDs(t, results, racers)

	if got := env.Balance(t, "src_many"); got != 0 {
		t.Errorf("src_many balance = %d, want 0 (50 x 100 debited exactly)", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM wallets WHERE id LIKE 'dst_many_%' AND balance = 100"); got != racers {
		t.Errorf("destinations credited with 100 = %d, want %d", got, racers)
	}
	if n, sum := ledgerFor(t, env, "src_many", "DEBIT"); n != racers || sum != racers*100 {
		t.Errorf("src_many DEBIT entries = %d totaling %d, want %d totaling %d", n, sum, racers, racers*100)
	}

	requireLedgerWellFormed(t, env, racers)
	if got := sumBalances(t, env); got != racers*100 {
		t.Errorf("sum of balances = %d, want %d (conservation of money)", got, racers*100)
	}
}

// TestConcurrentTransfersToSameDestination proves credits against one hot
// destination do not lose updates: eight concurrent transfers from distinct
// wallets all land, the destination balance reflects every one of them, and
// each contributes exactly one CREDIT row.
func TestConcurrentTransfersToSameDestination(t *testing.T) {
	const racers = 8

	env := testutil.NewEnv(t)
	for i := 0; i < racers; i++ {
		env.CreateWallet(t, fmt.Sprintf("src_shared_%d", i), 200)
	}
	env.CreateWallet(t, "dst_shared", 400)
	srv := newServer(t, env)

	results := race(t, srv, racers, func(i int) string {
		return transferBody(fmt.Sprintf("race-shared-%d", i), fmt.Sprintf("src_shared_%d", i), "dst_shared", 150)
	})
	requireNoTransportErrors(t, results)

	if got := statusCounts(results)[http.StatusCreated]; got != racers {
		t.Fatalf("201 responses = %d, want %d;%s", got, racers, describeFailures(results, http.StatusCreated))
	}
	requireUniqueTransferIDs(t, results, racers)

	for i := 0; i < racers; i++ {
		src := fmt.Sprintf("src_shared_%d", i)
		if got := env.Balance(t, src); got != 50 {
			t.Errorf("%s balance = %d, want 50", src, got)
		}
		if n, sum := ledgerFor(t, env, src, "DEBIT"); n != 1 || sum != 150 {
			t.Errorf("%s DEBIT entries = %d totaling %d, want 1 totaling 150", src, n, sum)
		}
	}
	if got := env.Balance(t, "dst_shared"); got != 400+racers*150 {
		t.Errorf("dst_shared balance = %d, want %d", got, 400+racers*150)
	}
	if n, sum := ledgerFor(t, env, "dst_shared", "CREDIT"); n != racers || sum != racers*150 {
		t.Errorf("dst_shared CREDIT entries = %d totaling %d, want %d totaling %d", n, sum, racers, racers*150)
	}

	requireLedgerWellFormed(t, env, racers)
	if got := sumBalances(t, env); got != 2000 {
		t.Errorf("sum of balances = %d, want 2000 (conservation of money)", got)
	}
}

// TestConcurrentSameKeyIdenticalPayload proves idempotency holds under a
// race, not just for sequential retries: ten simultaneous copies of one
// request produce ten 201 responses with byte-identical bodies, one transfer,
// one idempotency record, and money moved exactly once.
func TestConcurrentSameKeyIdenticalPayload(t *testing.T) {
	const racers = 10

	env := testutil.NewEnv(t)
	env.CreateWallet(t, "src_samekey", 1000)
	env.CreateWallet(t, "dst_samekey", 0)
	srv := newServer(t, env)

	const body = `{"idempotencyKey":"race-same-key","fromWalletId":"src_samekey","toWalletId":"dst_samekey","amount":100}`
	results := race(t, srv, racers, func(int) string { return body })
	requireNoTransportErrors(t, results)

	if got := statusCounts(results)[http.StatusCreated]; got != racers {
		t.Fatalf("201 responses = %d, want %d;%s", got, racers, describeFailures(results, http.StatusCreated))
	}
	// The winner's response and all nine replays are the same bytes.
	created := decodeTransfer(t, requireIdenticalBodies(t, bodiesWithStatus(results, http.StatusCreated)))
	if created.Status != string(constants.TransferProcessed) || created.Amount != 100 {
		t.Errorf("created transfer = %+v, want PROCESSED amount 100", created)
	}

	if got := env.Balance(t, "src_samekey"); got != 900 {
		t.Errorf("src_samekey balance = %d, want 900 (money moved once)", got)
	}
	if got := env.Balance(t, "dst_samekey"); got != 100 {
		t.Errorf("dst_samekey balance = %d, want 100", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM transfers"); got != 1 {
		t.Errorf("transfers = %d, want 1", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); got != 1 {
		t.Errorf("idempotency records = %d, want 1", got)
	}
	requireLedgerWellFormed(t, env, 1)
	if got := sumBalances(t, env); got != 1000 {
		t.Errorf("sum of balances = %d, want 1000 (conservation of money)", got)
	}

	// The transfer the losers were told about is real and retrievable.
	status, raw := get(t, srv, "/transfers/"+created.TransferID)
	if status != http.StatusOK {
		t.Fatalf("GET transfer %s: status %d, body %s", created.TransferID, status, raw)
	}
}

// TestConcurrentSameKeyDifferentPayloads proves that racing copies of the
// same key with different bodies resolve to exactly one winner and loud
// losers: one 201 and seven 409 KEY_REUSED responses. No loser may be
// silently answered with the winner's outcome, and only the winner's amount
// may have moved.
func TestConcurrentSameKeyDifferentPayloads(t *testing.T) {
	const racers = 8

	env := testutil.NewEnv(t)
	env.CreateWallet(t, "src_diffkey", 10000)
	env.CreateWallet(t, "dst_diffkey", 0)
	srv := newServer(t, env)

	results := race(t, srv, racers, func(i int) string {
		return transferBody("race-diff-key", "src_diffkey", "dst_diffkey", int64(101+i))
	})
	requireNoTransportErrors(t, results)

	counts := statusCounts(results)
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != racers-1 || len(counts) != 2 {
		t.Fatalf("status counts = %v, want exactly 1x201 and %dx409;%s",
			counts, racers-1, describeFailures(results, http.StatusCreated)+describeFailures(results, http.StatusConflict))
	}
	requireUniqueTransferIDs(t, results, 1)

	winner := decodeTransfer(t, requireIdenticalBodies(t, bodiesWithStatus(results, http.StatusCreated)))
	if winner.Amount < 101 || winner.Amount > 108 {
		t.Errorf("winner amount = %d, want one of 101..108", winner.Amount)
	}
	for _, raw := range bodiesWithStatus(results, http.StatusConflict) {
		if got := decodeError(t, raw).Error.Code; got != constants.CodeKeyReused {
			t.Errorf("409 error code = %q, want %q", got, constants.CodeKeyReused)
		}
	}

	if got := env.Balance(t, "src_diffkey"); got != 10000-winner.Amount {
		t.Errorf("src_diffkey balance = %d, want %d (only the winner moved money)", got, 10000-winner.Amount)
	}
	if got := env.Balance(t, "dst_diffkey"); got != winner.Amount {
		t.Errorf("dst_diffkey balance = %d, want %d", got, winner.Amount)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM transfers"); got != 1 {
		t.Errorf("transfers = %d, want 1", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); got != 1 {
		t.Errorf("idempotency records = %d, want 1", got)
	}
	requireLedgerWellFormed(t, env, 1)
	if got := env.CountRows(t, "SELECT coalesce(sum(amount), 0) FROM ledger_entries WHERE entry_type = 'DEBIT'"); got != winner.Amount {
		t.Errorf("debited total = %d, want the winner's amount %d", got, winner.Amount)
	}
	if got := sumBalances(t, env); got != 10000 {
		t.Errorf("sum of balances = %d, want 10000 (conservation of money)", got)
	}
}

// TestConcurrentSameKeyFailureReplayedToLosers proves that a recorded
// failure replays under a race exactly like a recorded success: eight
// identical concurrent requests against an underfunded wallet all get the
// same byte-identical 422, one FAILED transfer exists, and nothing moved.
func TestConcurrentSameKeyFailureReplayedToLosers(t *testing.T) {
	const racers = 8

	env := testutil.NewEnv(t)
	env.CreateWallet(t, "src_failkey", 50)
	env.CreateWallet(t, "dst_failkey", 0)
	srv := newServer(t, env)

	const body = `{"idempotencyKey":"race-fail-key","fromWalletId":"src_failkey","toWalletId":"dst_failkey","amount":100}`
	results := race(t, srv, racers, func(int) string { return body })
	requireNoTransportErrors(t, results)

	if got := statusCounts(results)[http.StatusUnprocessableEntity]; got != racers {
		t.Fatalf("422 responses = %d, want %d;%s", got, racers, describeFailures(results, http.StatusUnprocessableEntity))
	}
	recorded := decodeError(t, requireIdenticalBodies(t, bodiesWithStatus(results, http.StatusUnprocessableEntity)))
	if recorded.Error.Code != constants.CodeInsufficientFunds {
		t.Errorf("422 error code = %q, want %q", recorded.Error.Code, constants.CodeInsufficientFunds)
	}
	if got := env.CountRows(t, `SELECT count(*)
		FROM transfers
		WHERE id = $1::uuid AND status = 'FAILED' AND failure_reason = $2`,
		recorded.Error.TransferID, constants.FailureInsufficientFunds); got != 1 {
		t.Errorf("FAILED transfer %s: count = %d, want 1", recorded.Error.TransferID, got)
	}

	if got := env.Balance(t, "src_failkey"); got != 50 {
		t.Errorf("src_failkey balance = %d, want 50 (unchanged)", got)
	}
	if got := env.Balance(t, "dst_failkey"); got != 0 {
		t.Errorf("dst_failkey balance = %d, want 0 (unchanged)", got)
	}
	if got := countTransfers(t, env, string(constants.TransferFailed)); got != 1 {
		t.Errorf("FAILED transfers = %d, want 1", got)
	}
	if got := countTransfers(t, env, string(constants.TransferProcessed)); got != 0 {
		t.Errorf("PROCESSED transfers = %d, want 0", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); got != 0 {
		t.Errorf("ledger entries = %d, want 0 (a failed transfer moves nothing)", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); got != 1 {
		t.Errorf("idempotency records = %d, want 1", got)
	}
	if got := sumBalances(t, env); got != 50 {
		t.Errorf("sum of balances = %d, want 50 (conservation of money)", got)
	}
}

// TestInFlightSameKeyWaitsOnlyLockTimeoutThenKeyIsReusable pins the
// in-flight duplicate contract end to end. A duplicate that arrives while the
// key's transaction is still open waits - and the wait is bounded by
// lock_timeout, surfacing as 503 LOCK_TIMEOUT rather than blocking forever.
// Nothing of the uncommitted request may be observable. When the key holder
// rolls back without committing, its claim never existed, so the identical
// request is free to claim the key fresh and complete normally.
//
// The environment uses a 300ms lock_timeout so the test observes the
// production behavior in test time; only that constructor parameter differs.
func TestInFlightSameKeyWaitsOnlyLockTimeoutThenKeyIsReusable(t *testing.T) {
	env := testutil.NewEnvWithLockTimeout(t, 300)
	env.CreateWallet(t, "src_inflight", 1000)
	env.CreateWallet(t, "dst_inflight", 0)
	srv := newServer(t, env)

	const (
		key  = "race-inflight-key"
		body = `{"idempotencyKey":"race-inflight-key","fromWalletId":"src_inflight","toWalletId":"dst_inflight","amount":100}`
	)
	// The canonical bytes are what the HTTP layer would hand the store for the
	// same request; the holder claims the key exactly as a real request does.
	canonical, err := json.Marshal(dto.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   "src_inflight",
		ToWalletID:     "dst_inflight",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("marshal canonical request: %v", err)
	}

	// A holder transaction claims the key and stays open until released. It
	// then dies without committing, so the claim leaves no trace.
	holderDied := errors.New("holder intentionally rolled back")
	claimed := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- env.Store.InTx(context.Background(), func(ctx context.Context, tx service.Tx) error {
			ok, err := tx.ClaimIdempotency(ctx, key, canonical)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("holder's claim must win: nobody else touches this key first")
			}
			close(claimed)
			<-release
			return holderDied
		})
	}()
	<-claimed

	// The duplicate blocks on the conflicting claim until lock_timeout fires.
	start := time.Now()
	status, raw := post(t, srv, body)
	elapsed := time.Since(start)

	if status != http.StatusServiceUnavailable {
		t.Fatalf("in-flight duplicate: status %d, want 503 (body %s)", status, raw)
	}
	if got := decodeError(t, raw).Error.Code; got != constants.CodeLockTimeout {
		t.Errorf("error code = %q, want %q", got, constants.CodeLockTimeout)
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("duplicate returned after %s, want >= ~300ms (the configured lock_timeout)", elapsed)
	}

	// While the holder's claim is uncommitted, the duplicate left nothing
	// behind: no transfer, no balance change, and no visible idempotency row.
	if got := env.CountRows(t, "SELECT count(*) FROM transfers"); got != 0 {
		t.Errorf("transfers while the holder is uncommitted = %d, want 0", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); got != 0 {
		t.Errorf("idempotency rows while the holder is uncommitted = %d, want 0", got)
	}
	if got := env.Balance(t, "src_inflight"); got != 1000 {
		t.Errorf("src_inflight balance = %d, want 1000 (unchanged)", got)
	}

	// The holder dies without committing; its claim never happened.
	close(release)
	if err := <-holderDone; !errors.Is(err, holderDied) {
		t.Fatalf("holder transaction error = %v, want %v", err, holderDied)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); got != 0 {
		t.Errorf("idempotency rows after the holder rolled back = %d, want 0", got)
	}

	// The identical request now claims the key fresh and completes.
	status, raw = post(t, srv, body)
	if status != http.StatusCreated {
		t.Fatalf("post-rollback retry: status %d, want 201 (body %s)", status, raw)
	}
	if got := env.Balance(t, "src_inflight"); got != 900 {
		t.Errorf("src_inflight balance = %d, want 900", got)
	}
	if got := env.Balance(t, "dst_inflight"); got != 100 {
		t.Errorf("dst_inflight balance = %d, want 100", got)
	}
	if got := env.CountRows(t, "SELECT count(*) FROM transfers"); got != 1 {
		t.Errorf("transfers = %d, want 1", got)
	}
	requireLedgerWellFormed(t, env, 1)
	if got := sumBalances(t, env); got != 1000 {
		t.Errorf("sum of balances = %d, want 1000 (conservation of money)", got)
	}
}

// TestOppositeDirectionTransfersDoNotDeadlock proves the ordering rule under
// the race it exists for: transfers running in both directions between two
// wallets, so each transaction wants both rows. Because wallets are always
// locked in ascending id order, every request completes - a 503
// LOCK_TIMEOUT here would mean the lock order regressed. (The deterministic
// positive control for the detector itself is
// postgres.TestUnorderedRawLockingDeadlocks, which shows the same access
// pattern deadlocking when the order is not enforced.)
func TestOppositeDirectionTransfersDoNotDeadlock(t *testing.T) {
	const racers = 10

	env := testutil.NewEnv(t)
	env.CreateWallet(t, "lock_x", 1000)
	env.CreateWallet(t, "lock_y", 1000)
	srv := newServer(t, env)

	results := race(t, srv, racers, func(i int) string {
		if i%2 == 0 {
			return transferBody(fmt.Sprintf("race-deadlock-%d", i), "lock_x", "lock_y", 100)
		}
		return transferBody(fmt.Sprintf("race-deadlock-%d", i), "lock_y", "lock_x", 100)
	})
	requireNoTransportErrors(t, results)

	if got := statusCounts(results)[http.StatusCreated]; got != racers {
		t.Fatalf("201 responses = %d, want %d (any 503 here means the lock order regressed);%s",
			got, racers, describeFailures(results, http.StatusCreated))
	}
	requireUniqueTransferIDs(t, results, racers)

	// Five transfers left in each direction: the two flows cancel out exactly.
	if got := env.Balance(t, "lock_x"); got != 1000 {
		t.Errorf("lock_x balance = %d, want 1000 (5 out, 5 in)", got)
	}
	if got := env.Balance(t, "lock_y"); got != 1000 {
		t.Errorf("lock_y balance = %d, want 1000 (5 out, 5 in)", got)
	}
	if n, sum := ledgerFor(t, env, "lock_x", "DEBIT"); n != 5 || sum != 500 {
		t.Errorf("lock_x DEBIT entries = %d totaling %d, want 5 totaling 500", n, sum)
	}
	if n, sum := ledgerFor(t, env, "lock_y", "DEBIT"); n != 5 || sum != 500 {
		t.Errorf("lock_y DEBIT entries = %d totaling %d, want 5 totaling 500", n, sum)
	}

	requireLedgerWellFormed(t, env, racers)
	if got := sumBalances(t, env); got != 2000 {
		t.Errorf("sum of balances = %d, want 2000 (conservation of money)", got)
	}
}

// TestIndependentParallelTransfers proves throughput is not bought with
// isolation: 40 transfers across eight disjoint wallet pairs run fully in
// parallel, every one succeeds, each pair's books balance to the cent, and
// no transfer's effects leak into another pair's wallets.
func TestIndependentParallelTransfers(t *testing.T) {
	const (
		pairs = 8
		each  = 5
	)

	env := testutil.NewEnv(t)
	for i := 0; i < pairs; i++ {
		env.CreateWallet(t, fmt.Sprintf("ind_src_%d", i), 500)
		env.CreateWallet(t, fmt.Sprintf("ind_dst_%d", i), 0)
	}
	srv := newServer(t, env)

	racers := pairs * each
	results := race(t, srv, racers, func(i int) string {
		p := i % pairs
		return transferBody(fmt.Sprintf("race-ind-%d-%d", p, i/pairs),
			fmt.Sprintf("ind_src_%d", p), fmt.Sprintf("ind_dst_%d", p), 100)
	})
	requireNoTransportErrors(t, results)

	if got := statusCounts(results)[http.StatusCreated]; got != racers {
		t.Fatalf("201 responses = %d, want %d;%s", got, racers, describeFailures(results, http.StatusCreated))
	}
	requireUniqueTransferIDs(t, results, racers)

	for i := 0; i < pairs; i++ {
		src, dst := fmt.Sprintf("ind_src_%d", i), fmt.Sprintf("ind_dst_%d", i)
		if got := env.Balance(t, src); got != 0 {
			t.Errorf("%s balance = %d, want 0", src, got)
		}
		if got := env.Balance(t, dst); got != 500 {
			t.Errorf("%s balance = %d, want 500", dst, got)
		}
		if n, sum := ledgerFor(t, env, src, "DEBIT"); n != each || sum != 500 {
			t.Errorf("%s DEBIT entries = %d totaling %d, want %d totaling 500", src, n, sum, each)
		}
		if n, sum := ledgerFor(t, env, dst, "CREDIT"); n != each || sum != 500 {
			t.Errorf("%s CREDIT entries = %d totaling %d, want %d totaling 500", dst, n, sum, each)
		}
	}

	requireLedgerWellFormed(t, env, int64(racers))
	if got := sumBalances(t, env); got != pairs*500 {
		t.Errorf("sum of balances = %d, want %d (conservation of money)", got, pairs*500)
	}
}
