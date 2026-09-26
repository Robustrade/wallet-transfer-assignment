package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"wallet/internal/constants"
	"wallet/internal/dto"
	"wallet/internal/service"
	"wallet/internal/store/postgres"
	"wallet/tests/testutil"
)

// This file covers the transactional failure scenarios: every failure mode
// gets a test that pins the expected HTTP response and the expected database
// state (transfer, wallet, ledger, and idempotency record). Each test closes
// with requireDatabaseConsistent, the whole-database battery from the ledger
// integrity review (finding F-11.5).

// requireDatabaseConsistent runs the whole-database battery: an operation
// either left a complete, well-formed outcome behind or nothing at all.
// openings maps every wallet the test created to its starting balance
// (opening balances live outside the ledger, so they anchor the
// reconciliation).
func requireDatabaseConsistent(t *testing.T, env *testutil.Env, openings map[string]int64) {
	t.Helper()

	var openingTotal int64
	for _, opening := range openings {
		openingTotal += opening
	}

	if n := env.CountRows(t, "SELECT count(*) FROM wallets"); n != int64(len(openings)) {
		t.Errorf("wallet count = %d, want %d", n, len(openings))
	}

	var total int64
	for id, opening := range openings {
		balance := env.Balance(t, id)
		total += balance
		if balance < 0 {
			t.Errorf("wallet %s balance = %d, want >= 0", id, balance)
		}
		// Balance-ledger agreement: this wallet's balance moved exactly as
		// much as its ledger entries say it did.
		net := env.CountRows(t, `
			SELECT COALESCE(SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE -amount END), 0)
			FROM ledger_entries WHERE wallet_id = $1`, id)
		if balance-opening != net {
			t.Errorf("wallet %s: balance %d - opening %d = %d, but ledger net = %d",
				id, balance, opening, balance-opening, net)
		}
	}
	// Conservation: money is only ever moved, never created or destroyed.
	if total != openingTotal {
		t.Errorf("sum of balances = %d, want %d (sum of openings)", total, openingTotal)
	}

	// Once the request has returned, no transfer is still PENDING.
	if n := env.CountRows(t, "SELECT count(*) FROM transfers WHERE status = 'PENDING'"); n != 0 {
		t.Errorf("%d PENDING transfer(s) left behind, want 0", n)
	}
	// PROCESSED means the money moved, so both ledger sides must exist.
	if n := env.CountRows(t, `
		SELECT count(*) FROM transfers t
		WHERE t.status = 'PROCESSED'
		  AND (SELECT count(*) FROM ledger_entries e WHERE e.transfer_id = t.id) <> 2`); n != 0 {
		t.Errorf("%d PROCESSED transfer(s) without exactly 2 ledger entries", n)
	}
	// FAILED means no money moved, so nothing may point at it.
	if n := env.CountRows(t, `
		SELECT count(*) FROM transfers t
		WHERE t.status = 'FAILED'
		  AND EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id)`); n != 0 {
		t.Errorf("%d FAILED transfer(s) with ledger entries, want 0", n)
	}
	// Each entry mirrors its transfer: same amount, correct side.
	if n := env.CountRows(t, `
		SELECT count(*) FROM ledger_entries e
		JOIN transfers t ON t.id = e.transfer_id
		WHERE e.amount <> t.amount
		   OR (e.entry_type = 'DEBIT'  AND e.wallet_id <> t.from_wallet_id)
		   OR (e.entry_type = 'CREDIT' AND e.wallet_id <> t.to_wallet_id)`); n != 0 {
		t.Errorf("%d ledger entries disagree with their transfer", n)
	}
	if n := env.CountRows(t, `
		SELECT count(*) FROM ledger_entries e
		WHERE NOT EXISTS (SELECT 1 FROM transfers t WHERE t.id = e.transfer_id)`); n != 0 {
		t.Errorf("%d ledger entries attached to no transfer", n)
	}
	// Double-entry in aggregate: total debits equal total credits.
	if n := env.CountRows(t, `
		SELECT COALESCE(SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE -amount END), 0)
		FROM ledger_entries`); n != 0 {
		t.Errorf("ledger imbalance: debits - credits = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries WHERE amount <= 0"); n != 0 {
		t.Errorf("%d ledger entries with non-positive amount", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM transfers WHERE amount <= 0"); n != 0 {
		t.Errorf("%d transfers with non-positive amount", n)
	}
	// No wedged idempotency key: a surviving record must carry its outcome,
	// otherwise duplicates could never be replayed.
	if n := env.CountRows(t, "SELECT count(*) FROM idempotency_records WHERE response_status IS NULL"); n != 0 {
		t.Errorf("%d claimed-but-incomplete idempotency record(s), want 0", n)
	}
}

func requireStatus(t *testing.T, status int, raw []byte, want int) {
	t.Helper()
	if status != want {
		t.Fatalf("status = %d, want %d (body %s)", status, want, raw)
	}
}

func requireErrorCode(t *testing.T, raw []byte, want string) dto.ErrorBody {
	t.Helper()
	var resp dto.ErrorResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode error response: %v (body %s)", err, raw)
	}
	if resp.Error.Code != want {
		t.Fatalf("error code = %q, want %q (body %s)", resp.Error.Code, want, raw)
	}
	return resp.Error
}

// requireRecordedOutcome pins a complete idempotency record for key.
func requireRecordedOutcome(t *testing.T, env *testutil.Env, key string, status int) {
	t.Helper()
	n := env.CountRows(t, `
		SELECT count(*) FROM idempotency_records
		WHERE idempotency_key = $1 AND response_status = $2 AND response_body IS NOT NULL`,
		key, status)
	if n != 1 {
		t.Errorf("idempotency record for %q with status %d = %d, want 1", key, status, n)
	}
}

// TestFailureRejectedRequestsLeaveNoTrace covers invalid amounts, identical
// source and destination, malformed wallet references, and malformed keys.
// Every one is a 400 before any database work, and none may claim a key.
func TestFailureRejectedRequestsLeaveNoTrace(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	srv := newServer(t, env)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	cases := []struct {
		name string
		body string
	}{
		{"amount zero", `{"idempotencyKey":"f-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":0}`},
		{"amount negative", `{"idempotencyKey":"f-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":-100}`},
		{"amount above maximum", fmt.Sprintf(`{"idempotencyKey":"f-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":%d}`, constants.MaxAmount+1)},
		{"amount overflows int64", `{"idempotencyKey":"f-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":9223372036854775808}`},
		{"amount not a number", `{"idempotencyKey":"f-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":"100"}`},
		{"amount not an integer", `{"idempotencyKey":"f-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10.5}`},
		{"same source and destination", `{"idempotencyKey":"f-1","fromWalletId":"wallet_1","toWalletId":"wallet_1","amount":100}`},
		{"empty source wallet", `{"idempotencyKey":"f-1","fromWalletId":"","toWalletId":"wallet_2","amount":100}`},
		{"missing idempotencyKey", `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`},
		{"idempotencyKey with whitespace", `{"idempotencyKey":" f-1 ","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`},
		{"overlong wallet id", fmt.Sprintf(`{"idempotencyKey":"f-1","fromWalletId":%q,"toWalletId":"wallet_2","amount":100}`, strings.Repeat("w", constants.MaxIDLength+1))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := post(t, srv, tc.body)
			requireStatus(t, status, raw, http.StatusBadRequest)
			requireErrorCode(t, raw, constants.CodeValidation)

			// Expected database state: untouched, and the key never claimed.
			if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
				t.Errorf("transfers = %d, want 0", n)
			}
			if n := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); n != 0 {
				t.Errorf("idempotency records = %d, want 0 (400 must not claim the key)", n)
			}
			requireDatabaseConsistent(t, env, openings)
		})
	}
}

// TestFailureUnknownWalletIsRecordedWithoutEffects covers a well-formed
// request against a nonexistent wallet on either side: 404, no transfer, no
// ledger entry, no balance change, and the outcome recorded against the key.
func TestFailureUnknownWalletIsRecordedWithoutEffects(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"missing source wallet", "wallet_ghost", "wallet_2"},
		{"missing destination wallet", "wallet_1", "wallet_ghost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testutil.NewEnv(t)
			env.CreateWallet(t, "wallet_1", 1000)
			env.CreateWallet(t, "wallet_2", 500)
			srv := newServer(t, env)
			openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

			body := fmt.Sprintf(`{"idempotencyKey":"nf-1","fromWalletId":%q,"toWalletId":%q,"amount":100}`, tc.from, tc.to)

			status, raw := post(t, srv, body)
			requireStatus(t, status, raw, http.StatusNotFound)
			errBody := requireErrorCode(t, raw, constants.CodeWalletNotFound)
			if !strings.Contains(errBody.Message, "wallet_ghost") {
				t.Errorf("message = %q, want it to name the missing wallet", errBody.Message)
			}

			// Expected database state: nothing observable changed.
			if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
				t.Errorf("transfers = %d, want 0", n)
			}
			if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
				t.Errorf("ledger entries = %d, want 0", n)
			}
			if got := env.Balance(t, "wallet_1"); got != 1000 {
				t.Errorf("wallet_1 balance = %d, want 1000", got)
			}
			if got := env.Balance(t, "wallet_2"); got != 500 {
				t.Errorf("wallet_2 balance = %d, want 500", got)
			}
			requireDatabaseConsistent(t, env, openings)

			// The 404 is recorded: complete, and transfer-less.
			requireRecordedOutcome(t, env, "nf-1", http.StatusNotFound)
			if n := env.CountRows(t, `
				SELECT count(*) FROM idempotency_records
				WHERE idempotency_key = 'nf-1' AND transfer_id IS NULL`); n != 1 {
				t.Errorf("404 record with a transfer id = %d, want 0", n)
			}

			// A duplicate replays the recorded 404 byte-for-byte.
			status, replay := post(t, srv, body)
			requireStatus(t, status, replay, http.StatusNotFound)
			if string(replay) != string(raw) {
				t.Errorf("replay body differs:\n first: %s\nreplay: %s", raw, replay)
			}
			requireDatabaseConsistent(t, env, openings)
		})
	}
}

// TestFailureDuplicateKeyIsRefusedAndConsistent covers the same idempotencyKey
// arriving with a different payload: 409, no second transfer, money moved
// exactly once, and the original outcome still replayable.
func TestFailureDuplicateKeyIsRefusedAndConsistent(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	srv := newServer(t, env)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	const first = `{"idempotencyKey":"dup-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
	status, original := post(t, srv, first)
	requireStatus(t, status, original, http.StatusCreated)

	status, raw := post(t, srv, `{"idempotencyKey":"dup-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":200}`)
	requireStatus(t, status, raw, http.StatusConflict)
	requireErrorCode(t, raw, constants.CodeKeyReused)

	// Expected database state: exactly the first transfer happened.
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 2 {
		t.Errorf("ledger entries = %d, want 2", n)
	}
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance = %d, want 900 (the 409 must not move money)", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 600 {
		t.Errorf("wallet_2 balance = %d, want 600", got)
	}
	requireDatabaseConsistent(t, env, openings)

	// The recorded outcome still replays for the request that owns the key.
	status, replay := post(t, srv, first)
	requireStatus(t, status, replay, http.StatusCreated)
	if string(replay) != string(original) {
		t.Errorf("replay body differs:\n first: %s\nreplay: %s", original, replay)
	}
	requireDatabaseConsistent(t, env, openings)
}

// TestFailureInsufficientFundsLeavesOnlyAFailedTransfer covers an amount the
// source cannot cover: a recorded 422, one terminal FAILED transfer, no money
// moved, no ledger entry.
func TestFailureInsufficientFundsLeavesOnlyAFailedTransfer(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 50)
	env.CreateWallet(t, "wallet_2", 500)
	srv := newServer(t, env)
	openings := map[string]int64{"wallet_1": 50, "wallet_2": 500}

	const body = `{"idempotencyKey":"ins-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, raw := post(t, srv, body)
	requireStatus(t, status, raw, http.StatusUnprocessableEntity)
	errBody := requireErrorCode(t, raw, constants.CodeInsufficientFunds)
	if errBody.TransferID == "" {
		t.Fatal("INSUFFICIENT_FUNDS response must carry the FAILED transfer id")
	}
	if want := "wallet wallet_1 has insufficient funds: balance 50, requested 100"; errBody.Message != want {
		t.Errorf("message = %q, want %q", errBody.Message, want)
	}

	// Expected transfer state: one FAILED transfer, matching the response.
	if n := env.CountRows(t, `
		SELECT count(*) FROM transfers
		WHERE id = $1::uuid AND from_wallet_id = 'wallet_1' AND to_wallet_id = 'wallet_2'
		  AND amount = 100 AND status = 'FAILED' AND failure_reason = 'INSUFFICIENT_FUNDS'`,
		errBody.TransferID); n != 1 {
		t.Errorf("FAILED transfer matching the response = %d, want 1", n)
	}
	// Expected wallet and ledger state: no money moved, no entries.
	if got := env.Balance(t, "wallet_1"); got != 50 {
		t.Errorf("wallet_1 balance = %d, want 50", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 500 {
		t.Errorf("wallet_2 balance = %d, want 500", got)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
		t.Errorf("ledger entries = %d, want 0", n)
	}
	requireDatabaseConsistent(t, env, openings)

	// The 422 is recorded: a duplicate replays it and creates no second
	// transfer.
	status, replay := post(t, srv, body)
	requireStatus(t, status, replay, http.StatusUnprocessableEntity)
	if string(replay) != string(raw) {
		t.Errorf("replay body differs:\n first: %s\nreplay: %s", raw, replay)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1", n)
	}
	requireDatabaseConsistent(t, env, openings)
}

// TestFailureWalletUpdateRollsBackEverything injects a failure into each
// balance update. The debit case fails before any money moved; the credit case
// fails after the debit succeeded, so it proves the debit is undone too.
func TestFailureWalletUpdateRollsBackEverything(t *testing.T) {
	for _, tc := range []struct{ name, method string }{
		{"debit statement fails", "DebitWallet"},
		{"credit statement fails after the debit", "CreditWallet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testutil.NewEnv(t)
			env.CreateWallet(t, "wallet_1", 1000)
			env.CreateWallet(t, "wallet_2", 500)
			openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

			fault := &faultStore{Store: env.Store, mode: faultTxMethod, method: tc.method,
				err: errors.New("injected: wallet update failed")}
			srv := newServerOn(t, fault, fault.Ping)

			const body = `{"idempotencyKey":"wu-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

			status, raw := post(t, srv, body)
			requireStatus(t, status, raw, http.StatusInternalServerError)
			errBody := requireErrorCode(t, raw, constants.CodeInternal)
			if errBody.Message != "internal error" {
				t.Errorf("message = %q, want the fixed, detail-free %q", errBody.Message, "internal error")
			}

			// Expected database state: every step of the attempt undone.
			if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
				t.Errorf("transfers = %d, want 0", n)
			}
			if got := env.Balance(t, "wallet_1"); got != 1000 {
				t.Errorf("wallet_1 balance = %d, want 1000", got)
			}
			if got := env.Balance(t, "wallet_2"); got != 500 {
				t.Errorf("wallet_2 balance = %d, want 500", got)
			}
			if n := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); n != 0 {
				t.Errorf("idempotency records = %d, want 0 (the key was not claimed)", n)
			}
			requireDatabaseConsistent(t, env, openings)

			// The key was not burned: the identical request succeeds now.
			status, raw = post(t, srv, body)
			requireStatus(t, status, raw, http.StatusCreated)
			if got := env.Balance(t, "wallet_1"); got != 900 {
				t.Errorf("wallet_1 balance after retry = %d, want 900", got)
			}
			if got := env.Balance(t, "wallet_2"); got != 600 {
				t.Errorf("wallet_2 balance after retry = %d, want 600", got)
			}
			requireRecordedOutcome(t, env, "wu-1", http.StatusCreated)
			requireDatabaseConsistent(t, env, openings)
		})
	}
}

// TestFailureLedgerInsertionRollsBackBothBalances injects a failure after both
// balance updates succeeded: the ledger pair, the balances, the transfer row,
// and the idempotency claim must all disappear together.
func TestFailureLedgerInsertionRollsBackBothBalances(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	fault := &faultStore{Store: env.Store, mode: faultTxMethod, method: "InsertLedgerEntries",
		err: errors.New("injected: ledger insert failed")}
	srv := newServerOn(t, fault, fault.Ping)

	const body = `{"idempotencyKey":"li-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, raw := post(t, srv, body)
	requireStatus(t, status, raw, http.StatusInternalServerError)
	requireErrorCode(t, raw, constants.CodeInternal)

	// The debit and the credit both happened before the injected failure, and
	// both must be rolled back.
	if got := env.Balance(t, "wallet_1"); got != 1000 {
		t.Errorf("wallet_1 balance = %d, want 1000 (debit rolled back)", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 500 {
		t.Errorf("wallet_2 balance = %d, want 500 (credit rolled back)", got)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
		t.Errorf("ledger entries = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
		t.Errorf("transfers = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); n != 0 {
		t.Errorf("idempotency records = %d, want 0", n)
	}
	requireDatabaseConsistent(t, env, openings)

	// Retry through the same server: now it completes, exactly once.
	status, raw = post(t, srv, body)
	requireStatus(t, status, raw, http.StatusCreated)
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance after retry = %d, want 900", got)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 2 {
		t.Errorf("ledger entries after retry = %d, want 2", n)
	}
	requireDatabaseConsistent(t, env, openings)
}

// TestFailureStatusUpdateRollsBackEverything injects a failure into the status
// update of each branch: MarkProcessed (money already moved) and MarkFailed
// (insufficient funds already detected). Both must roll everything back.
func TestFailureStatusUpdateRollsBackEverything(t *testing.T) {
	cases := []struct {
		name      string
		balance   int64
		method    string
		wantRetry int
	}{
		{"processed update fails", 1000, "MarkProcessed", http.StatusCreated},
		{"failed update fails", 50, "MarkFailed", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := testutil.NewEnv(t)
			env.CreateWallet(t, "wallet_1", tc.balance)
			env.CreateWallet(t, "wallet_2", 500)
			openings := map[string]int64{"wallet_1": tc.balance, "wallet_2": 500}

			fault := &faultStore{Store: env.Store, mode: faultTxMethod, method: tc.method,
				err: errors.New("injected: transfer status update failed")}
			srv := newServerOn(t, fault, fault.Ping)

			const body = `{"idempotencyKey":"su-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

			status, raw := post(t, srv, body)
			requireStatus(t, status, raw, http.StatusInternalServerError)
			requireErrorCode(t, raw, constants.CodeInternal)

			// Expected database state: no transfer in any state, no money
			// moved, no outcome recorded.
			if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
				t.Errorf("transfers = %d, want 0", n)
			}
			if got := env.Balance(t, "wallet_1"); got != tc.balance {
				t.Errorf("wallet_1 balance = %d, want %d", got, tc.balance)
			}
			if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
				t.Errorf("ledger entries = %d, want 0", n)
			}
			if n := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); n != 0 {
				t.Errorf("idempotency records = %d, want 0", n)
			}
			requireDatabaseConsistent(t, env, openings)

			// The retry runs the intact path and lands the real outcome.
			status, raw = post(t, srv, body)
			requireStatus(t, status, raw, tc.wantRetry)
			if tc.wantRetry == http.StatusCreated {
				if got := env.Balance(t, "wallet_1"); got != tc.balance-100 {
					t.Errorf("wallet_1 balance after retry = %d, want %d", got, tc.balance-100)
				}
				if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 2 {
					t.Errorf("ledger entries after retry = %d, want 2", n)
				}
			} else {
				if got := env.Balance(t, "wallet_1"); got != tc.balance {
					t.Errorf("wallet_1 balance after retry = %d, want %d (still no funds)", got, tc.balance)
				}
				if n := env.CountRows(t, `
					SELECT count(*) FROM transfers
					WHERE status = 'FAILED' AND failure_reason = 'INSUFFICIENT_FUNDS'`); n != 1 {
					t.Errorf("FAILED transfers after retry = %d, want 1", n)
				}
				if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
					t.Errorf("ledger entries after retry = %d, want 0", n)
				}
			}
			requireDatabaseConsistent(t, env, openings)
		})
	}
}

// TestFailureIdempotencyCompletionIsAtomic injects a failure into recording
// the outcome after the transfer itself succeeded: the commit never happens,
// so the 201 must never reach the client and the key must stay reusable.
func TestFailureIdempotencyCompletionIsAtomic(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	fault := &faultStore{Store: env.Store, mode: faultTxMethod, method: "CompleteIdempotency",
		err: errors.New("injected: recording the outcome failed")}
	srv := newServerOn(t, fault, fault.Ping)

	const body = `{"idempotencyKey":"ci-2","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, raw := post(t, srv, body)
	requireStatus(t, status, raw, http.StatusInternalServerError)
	requireErrorCode(t, raw, constants.CodeInternal)

	// The transfer had already moved money and been marked PROCESSED; all of
	// it must be gone, because the outcome was never recorded.
	if got := env.Balance(t, "wallet_1"); got != 1000 {
		t.Errorf("wallet_1 balance = %d, want 1000", got)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
		t.Errorf("transfers = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
		t.Errorf("ledger entries = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); n != 0 {
		t.Errorf("idempotency records = %d, want 0", n)
	}
	requireDatabaseConsistent(t, env, openings)

	// Retry succeeds, and a further duplicate replays the retry's bytes.
	status, first := post(t, srv, body)
	requireStatus(t, status, first, http.StatusCreated)
	requireRecordedOutcome(t, env, "ci-2", http.StatusCreated)

	status, replay := post(t, srv, body)
	requireStatus(t, status, replay, http.StatusCreated)
	if string(replay) != string(first) {
		t.Errorf("replay body differs:\n first: %s\nreplay: %s", first, replay)
	}
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance after retries = %d, want 900 (moved once)", got)
	}
	requireDatabaseConsistent(t, env, openings)
}

// TestFailureDatabaseConnectionLossIsContained runs the API against a store
// whose pool is closed: requests fail as 503 (transport failure, retry-safe)
// without leaking details, health reports 503, and the database is untouched.
// A healthy server then processes the same request.
func TestFailureDatabaseConnectionLossIsContained(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	broken, err := postgres.New(context.Background(), testutil.DatabaseURL(), 4, 2000)
	if err != nil {
		t.Fatalf("connect the store to break: %v", err)
	}
	t.Cleanup(broken.Close)
	broken.Close() // the database becomes unreachable for this server

	srv := newServerOn(t, broken, broken.Ping)
	const body = `{"idempotencyKey":"db-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, raw := post(t, srv, body)
	requireStatus(t, status, raw, http.StatusServiceUnavailable)
	errBody := requireErrorCode(t, raw, constants.CodeDatabaseUnavailable)
	if strings.Contains(errBody.Message, "closed pool") || strings.Contains(errBody.Message, "begin transaction") {
		t.Errorf("message = %q, must not leak internal details", errBody.Message)
	}

	if status, raw := get(t, srv, "/wallets/wallet_1"); status != http.StatusServiceUnavailable {
		t.Errorf("GET wallet: status %d, want 503 (body %s)", status, raw)
	}
	if status, raw := get(t, srv, "/healthz"); status != http.StatusServiceUnavailable {
		t.Errorf("GET /healthz: status %d, want 503 (body %s)", status, raw)
	}

	// Expected database state: nothing changed while the database was "down".
	requireDatabaseConsistent(t, env, openings)

	// A healthy server processes the same request exactly once.
	healthy := newServer(t, env)
	status, raw = post(t, healthy, body)
	requireStatus(t, status, raw, http.StatusCreated)
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance = %d, want 900", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 600 {
		t.Errorf("wallet_2 balance = %d, want 600", got)
	}
	requireDatabaseConsistent(t, env, openings)
}

// TestFailureCommitLandedIsResolvedByReadBack covers the first commit-in-doubt
// branch: the COMMIT reached the server, but its confirmation was lost. The
// client must get the recorded outcome (201), and money must have moved
// exactly once.
func TestFailureCommitLandedIsResolvedByReadBack(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	fault := &faultStore{Store: env.Store, mode: faultCommitLanded}
	srv := newServerOn(t, fault, fault.Ping)

	const body = `{"idempotencyKey":"ci-3","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, raw := post(t, srv, body)
	requireStatus(t, status, raw, http.StatusCreated)
	var created dto.TransferResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode transfer response: %v (body %s)", err, raw)
	}
	if created.Status != string(constants.TransferProcessed) || created.Amount != 100 {
		t.Errorf("transfer = %+v, want PROCESSED amount 100", created)
	}

	// The transfer was in fact committed, and nothing else happened: one
	// transfer, one ledger pair, one balance move.
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 2 {
		t.Errorf("ledger entries = %d, want 2", n)
	}
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance = %d, want 900", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 600 {
		t.Errorf("wallet_2 balance = %d, want 600", got)
	}
	requireRecordedOutcome(t, env, "ci-3", http.StatusCreated)
	requireDatabaseConsistent(t, env, openings)

	// The 201 came from reading the record back: an identical duplicate
	// replays the exact same bytes.
	status, replay := post(t, srv, body)
	requireStatus(t, status, replay, http.StatusCreated)
	if string(replay) != string(raw) {
		t.Errorf("replay body differs:\n first: %s\nreplay: %s", raw, replay)
	}
	requireDatabaseConsistent(t, env, openings)
}

// TestFailureCommitNeverLandedIsReportedUnknown covers the second
// commit-in-doubt branch: nothing reached the server. The client gets 503
// OUTCOME_UNKNOWN, the database is untouched, and the identical retry with the
// same key safely performs the transfer.
func TestFailureCommitNeverLandedIsReportedUnknown(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	fault := &faultStore{Store: env.Store, mode: faultNothingLanded}
	srv := newServerOn(t, fault, fault.Ping)

	const body = `{"idempotencyKey":"cu-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	status, raw := post(t, srv, body)
	requireStatus(t, status, raw, http.StatusServiceUnavailable)
	requireErrorCode(t, raw, constants.CodeOutcomeUnknown)

	// Expected database state: nothing at all - no transfer, no ledger entry,
	// no record, balances untouched.
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
		t.Errorf("transfers = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
		t.Errorf("ledger entries = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM idempotency_records"); n != 0 {
		t.Errorf("idempotency records = %d, want 0", n)
	}
	requireDatabaseConsistent(t, env, openings)

	// The retry with the same key is safe and performs the transfer once.
	status, raw = post(t, srv, body)
	requireStatus(t, status, raw, http.StatusCreated)
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance after retry = %d, want 900", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 600 {
		t.Errorf("wallet_2 balance after retry = %d, want 600", got)
	}
	requireRecordedOutcome(t, env, "cu-1", http.StatusCreated)
	requireDatabaseConsistent(t, env, openings)
}

// TestFailureCommitLandedIsResolvedWhenClientIsGone covers the read-back under
// client cancellation: the commit landed, then the client went away before the
// outcome could be reported. Resolving the outcome is the service's own
// responsibility, so the read-back must run detached from the dead request
// context, find the committed record, and hand the recorded outcome back -
// with money moved exactly once.
func TestFailureCommitLandedIsResolvedWhenClientIsGone(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "wallet_1", 1000)
	env.CreateWallet(t, "wallet_2", 500)
	openings := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cancelOnCommitStore{Store: env.Store, cancel: cancel}
	svc := service.New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := svc.Execute(ctx, dto.TransferRequest{
		IdempotencyKey: "gone-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("Execute: %v (the commit landed; the outcome must resolve by read-back)", err)
	}
	if result.Status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", result.Status, result.Body)
	}
	var created dto.TransferResponse
	if err := json.Unmarshal(result.Body, &created); err != nil {
		t.Fatalf("decode transfer response: %v (body %s)", err, result.Body)
	}
	if created.Status != string(constants.TransferProcessed) || created.Amount != 100 {
		t.Errorf("transfer = %+v, want PROCESSED amount 100", created)
	}

	// The commit landed exactly once: one transfer, one ledger pair, one
	// balance move, and a complete record for future retries.
	if got := env.Balance(t, "wallet_1"); got != 900 {
		t.Errorf("wallet_1 balance = %d, want 900", got)
	}
	if got := env.Balance(t, "wallet_2"); got != 600 {
		t.Errorf("wallet_2 balance = %d, want 600", got)
	}
	requireRecordedOutcome(t, env, "gone-1", http.StatusCreated)
	requireDatabaseConsistent(t, env, openings)
}
