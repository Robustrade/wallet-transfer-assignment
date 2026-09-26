package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"wallet/internal/constants"
	"wallet/internal/domain"
	"wallet/internal/service"
	"wallet/tests/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Run(m)) }

// requireNotRetrySafe asserts an invariant violation (a rowcount assertion
// tripping) stays a plain server-side error: it must never match the
// retry-safe sentinels mapError produces, because the API would then answer
// 503 "retry" for what is actually a bug.
func requireNotRetrySafe(t *testing.T, err error) {
	t.Helper()
	for _, sentinel := range []error{
		domain.ErrLockTimeout, domain.ErrStatementTimeout, domain.ErrDatabaseUnavailable,
	} {
		if errors.Is(err, sentinel) {
			t.Errorf("invariant violation %v must not match retry-safe %v", err, sentinel)
		}
	}
}

// TestIdempotencyClaimConflictAndReplay covers the storage half of the
// idempotency contract: the first claim wins, a second claim on the same key
// loses, and the stored request body is compared with jsonb semantics.
func TestIdempotencyClaimConflictAndReplay(t *testing.T) {
	env := testutil.NewEnv(t)
	ctx := context.Background()

	canonical := []byte(`{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":100}`)
	other := []byte(`{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":500}`)

	// First request claims the key and records a 404 outcome (no transfer).
	err := env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		claimed, err := tx.ClaimIdempotency(ctx, "k1", canonical)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatal("first claim must succeed")
		}
		return tx.CompleteIdempotency(ctx, "k1", nil, 404, []byte(`{"error":"not found"}`))
	})
	if err != nil {
		t.Fatalf("first transaction failed: %v", err)
	}

	// A duplicate sees the conflict and replays the stored outcome.
	err = env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		claimed, err := tx.ClaimIdempotency(ctx, "k1", canonical)
		if err != nil {
			return err
		}
		if claimed {
			t.Fatal("duplicate claim must not succeed")
		}
		rec, found, err := tx.GetIdempotencyRecord(ctx, "k1", canonical)
		if err != nil || !found {
			t.Fatalf("read record: found=%v err=%v", found, err)
		}
		if !rec.RequestMatches {
			t.Fatal("identical request must compare as a match")
		}
		if !rec.Complete() || *rec.ResponseStatus != 404 {
			t.Fatalf("want complete 404 record, got %+v", rec)
		}
		if string(rec.ResponseBody) != `{"error":"not found"}` {
			t.Fatalf("unexpected stored body %q", rec.ResponseBody)
		}
		if rec.TransferID != nil {
			t.Fatalf("404 outcome must not reference a transfer, got %v", *rec.TransferID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("duplicate transaction failed: %v", err)
	}

	// The same key with a different payload must not match (no replay).
	rec, found, err := env.Store.GetIdempotencyRecord(ctx, "k1", other)
	if err != nil || !found {
		t.Fatalf("read record: found=%v err=%v", found, err)
	}
	if rec.RequestMatches {
		t.Fatal("different payload must not compare as a match")
	}
}

func TestIdempotencyCompletedTransferOutcomesRequireTransferID(t *testing.T) {
	env := testutil.NewEnv(t)
	ctx := context.Background()

	for _, status := range []int{201, 422} {
		_, err := env.Pool.Exec(ctx, `
INSERT INTO idempotency_records (idempotency_key, request_body, response_status, response_body)
VALUES ($1, '{}'::jsonb, $2, '{}')`, fmt.Sprintf("missing-transfer-%d", status), status)
		if err == nil {
			t.Errorf("response status %d without transfer_id must violate the idempotency constraint", status)
		}
	}
}

// TestLockWalletsIsDeadlockFreeRegardlessOfInputOrder proves the store-level
// half of the deadlock-freedom argument: even when callers pass wallet ids in
// opposite orders, rows are locked in one global order, so both transactions
// complete.
func TestLockWalletsIsDeadlockFreeRegardlessOfInputOrder(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "w_ord_a", 100)
	env.CreateWallet(t, "w_ord_b", 100)

	orders := [][]string{{"w_ord_a", "w_ord_b"}, {"w_ord_b", "w_ord_a"}}
	start := make(chan struct{})
	errs := make(chan error, len(orders))

	for _, ids := range orders {
		go func(ids []string) {
			<-start
			errs <- env.Store.InTx(context.Background(), func(ctx context.Context, tx service.Tx) error {
				if _, err := tx.LockWallets(ctx, ids); err != nil {
					return err
				}
				// Hold the locks long enough for the other transaction to
				// reach its own lock attempt.
				time.Sleep(200 * time.Millisecond)
				return nil
			})
		}(ids)
	}
	close(start)

	for range orders {
		if err := <-errs; err != nil {
			t.Fatalf("ordered locking must not deadlock, got: %v", err)
		}
	}
}

// TestUnorderedRawLockingDeadlocks is a positive control for the test above:
// with the same harness but opposite lock orders, PostgreSQL's deadlock
// detector kills one transaction with SQLSTATE 40P01. If this control ever
// stops detecting the deadlock, the "no deadlock" result above proves nothing.
func TestUnorderedRawLockingDeadlocks(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "w_ctl_a", 100)
	env.CreateWallet(t, "w_ctl_b", 100)

	ctx := context.Background()
	c1, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn 1: %v", err)
	}
	defer c1.Release()
	c2, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn 2: %v", err)
	}
	defer c2.Release()

	tx1, err := c1.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(context.WithoutCancel(ctx))
	tx2, err := c2.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	defer tx2.Rollback(context.WithoutCancel(ctx))

	lock := func(tx interface {
		Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	}, id string) error {
		_, err := tx.Exec(ctx, "SELECT 1 FROM wallets WHERE id = $1 FOR NO KEY UPDATE", id)
		return err
	}

	// Opposite first locks...
	if err := lock(tx1, "w_ctl_a"); err != nil {
		t.Fatalf("tx1 first lock: %v", err)
	}
	if err := lock(tx2, "w_ctl_b"); err != nil {
		t.Fatalf("tx2 first lock: %v", err)
	}

	// ...then opposite second locks, which wait on each other.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, step := range []struct {
		tx interface {
			Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
		}
		id string
	}{{tx1, "w_ctl_b"}, {tx2, "w_ctl_a"}} {
		wg.Add(1)
		go func(i int, step struct {
			tx interface {
				Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
			}
			id string
		}) {
			defer wg.Done()
			errs[i] = lock(step.tx, step.id)
		}(i, step)
	}
	wg.Wait()

	deadlocks := 0
	for _, err := range errs {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
			deadlocks++
		}
	}
	if deadlocks == 0 {
		t.Fatalf("expected a deadlock (40P01) from unordered locking, got %v", errs)
	}
}

// TestDebitGuardNeverLetsBalanceGoNegative covers the insufficient-funds
// guard: the update is a no-op when the balance is too low, so the check
// cannot be bypassed by application bugs.
func TestDebitGuardNeverLetsBalanceGoNegative(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "w_debit", 100)
	ctx := context.Background()

	err := env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		ok, err := tx.DebitWallet(ctx, "w_debit", 60)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("debit of 60 from a balance of 100 must succeed")
		}
		ok, err = tx.DebitWallet(ctx, "w_debit", 60)
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("second debit of 60 from a balance of 40 must not succeed")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}
	if got := env.Balance(t, "w_debit"); got != 40 {
		t.Fatalf("balance = %d, want 40", got)
	}
}

// TestLedgerEntriesArePinnedToTheirTransfer covers the composite foreign key:
// a ledger entry cannot reference a transfer with a different amount, and a
// transfer cannot have two entries of the same type.
func TestLedgerEntriesArePinnedToTheirTransfer(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "w_led_a", 1000)
	env.CreateWallet(t, "w_led_b", 0)
	ctx := context.Background()

	var transferID string
	err := env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		tr, err := tx.CreateTransfer(ctx, "w_led_a", "w_led_b", 100)
		if err != nil {
			return err
		}
		transferID = tr.ID
		return tx.InsertLedgerEntries(ctx, tr.ID, "w_led_a", "w_led_b", 999) // wrong amount
	})
	if err == nil {
		t.Fatal("ledger entry with a mismatched amount must be rejected")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("want foreign_key_violation (23503), got %v", err)
	}
	// The failed transaction must have left nothing behind.
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
		t.Fatalf("ledger_entries rows after rollback = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
		t.Fatalf("transfers rows after rollback = %d, want 0", n)
	}

	// With the matching amount the pair is written; writing it again is
	// rejected by the unique (transfer_id, entry_type) constraint.
	err = env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		tr, err := tx.CreateTransfer(ctx, "w_led_a", "w_led_b", 100)
		if err != nil {
			return err
		}
		transferID = tr.ID
		if err := tx.InsertLedgerEntries(ctx, tr.ID, "w_led_a", "w_led_b", 100); err != nil {
			return err
		}
		return tx.InsertLedgerEntries(ctx, tr.ID, "w_led_a", "w_led_b", 100) // duplicate
	})
	if err == nil {
		t.Fatal("duplicate ledger entries must be rejected")
	}
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want unique_violation (23505), got %v", err)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries WHERE transfer_id = $1::uuid", transferID); n != 0 {
		t.Fatalf("ledger_entries rows for rolled-back transfer = %d, want 0", n)
	}
}

func TestLedgerEntriesMustUseTheirTransfersWalletRoles(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "w_role_source", 1000)
	env.CreateWallet(t, "w_role_destination", 0)
	env.CreateWallet(t, "w_role_other", 0)
	ctx := context.Background()

	var transferID string
	if err := env.Pool.QueryRow(ctx, `
INSERT INTO transfers (from_wallet_id, to_wallet_id, amount)
VALUES ('w_role_source', 'w_role_destination', 100)
RETURNING id::text`).Scan(&transferID); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	for _, entry := range []struct {
		name string
		typ  string
	}{
		{name: "debit must use source wallet", typ: "DEBIT"},
		{name: "credit must use destination wallet", typ: "CREDIT"},
	} {
		t.Run(entry.name, func(t *testing.T) {
			_, err := env.Pool.Exec(ctx, `
INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
VALUES ($1::uuid, 'w_role_other', $2, 100)`, transferID, entry.typ)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("wrong-wallet %s entry must be rejected with check_violation (23514), got %v", entry.typ, err)
			}
		})
	}

	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries WHERE transfer_id = $1::uuid", transferID); n != 0 {
		t.Fatalf("invalid ledger entries committed = %d, want 0", n)
	}
}

// TestTransferStateMachineIsTerminal covers the guarded status updates: once
// a transfer leaves PENDING it can never be moved again.
func TestTransferStateMachineIsTerminal(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "w_sm_a", 1000)
	env.CreateWallet(t, "w_sm_b", 0)
	ctx := context.Background()

	var transferID string
	err := env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		tr, err := tx.CreateTransfer(ctx, "w_sm_a", "w_sm_b", 100)
		if err != nil {
			return err
		}
		transferID = tr.ID
		if tr.Status != constants.TransferPending {
			t.Fatalf("new transfer status = %s, want PENDING", tr.Status)
		}
		// Insert the required ledger pair so the deferred trigger that
		// enforces "PROCESSED transfers must have exactly one DEBIT and one
		// CREDIT" does not reject the commit.
		if _, err := tx.DebitWallet(ctx, "w_sm_a", 100); err != nil {
			return err
		}
		if err := tx.CreditWallet(ctx, "w_sm_b", 100); err != nil {
			return err
		}
		if err := tx.InsertLedgerEntries(ctx, tr.ID, "w_sm_a", "w_sm_b", 100); err != nil {
			return err
		}
		if err := tx.MarkProcessed(ctx, tr.ID); err != nil {
			return err
		}
		// PROCESSED is terminal: both further transitions must fail with the
		// plain rowcount-assertion error, never a retry-safe sentinel.
		if err := tx.MarkProcessed(ctx, tr.ID); err == nil {
			t.Fatal("re-marking a PROCESSED transfer must fail")
		} else {
			requireNotRetrySafe(t, err)
		}
		if err := tx.MarkFailed(ctx, tr.ID, constants.FailureInsufficientFunds); err == nil {
			t.Fatal("failing a PROCESSED transfer must fail")
		} else {
			requireNotRetrySafe(t, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	var status string
	if err := env.Pool.QueryRow(ctx, "SELECT status FROM transfers WHERE id = $1::uuid", transferID).Scan(&status); err != nil {
		t.Fatalf("read transfer: %v", err)
	}
	if status != string(constants.TransferProcessed) {
		t.Fatalf("status = %s, want PROCESSED", status)
	}
}

// TestCompleteIdempotencyIsWriteOnce pins the response_status IS NULL guard on
// the completion UPDATE: once a key has a recorded outcome, completing it
// again matches zero rows, fails the rowcount assertion, and leaves the
// recorded outcome untouched.
func TestCompleteIdempotencyIsWriteOnce(t *testing.T) {
	env := testutil.NewEnv(t)
	ctx := context.Background()

	canonical := []byte(`{"idempotencyKey":"k_once","fromWalletId":"w1","toWalletId":"w2","amount":100}`)
	err := env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		claimed, err := tx.ClaimIdempotency(ctx, "k_once", canonical)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatal("first claim must succeed")
		}
		return tx.CompleteIdempotency(ctx, "k_once", nil, 404, []byte(`{"error":"first"}`))
	})
	if err != nil {
		t.Fatalf("first completion failed: %v", err)
	}

	// Completing the already-complete key must fail loudly...
	err = env.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
		return tx.CompleteIdempotency(ctx, "k_once", nil, 404, []byte(`{"error":"second"}`))
	})
	if err == nil {
		t.Fatal("completing an already-complete record must fail")
	}
	requireNotRetrySafe(t, err)

	// ...and the recorded outcome must still be the first one.
	rec, found, err := env.Store.GetIdempotencyRecord(ctx, "k_once", canonical)
	if err != nil || !found {
		t.Fatalf("read record: found=%v err=%v", found, err)
	}
	if string(rec.ResponseBody) != `{"error":"first"}` {
		t.Fatalf("recorded body = %q, want the first outcome", rec.ResponseBody)
	}
}

// TestSchemaRejectsImpossibleTransfers pins the database-level backstops that
// the HTTP API can never reach. The service always debits through the guard
// clause and always writes well-formed rows; these raw statements prove that
// even a buggy (or future) caller cannot commit an inconsistent state - the
// constraints reject it. Each case asserts both the SQL state and the specific
// constraint that fired, so a renamed or dropped constraint is caught here.
func TestSchemaRejectsImpossibleTransfers(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateWallet(t, "w_c1", 100)
	env.CreateWallet(t, "w_c2", 100)
	ctx := context.Background()

	cases := []struct {
		name       string
		sql        string
		wantCode   string
		wantConstr string
	}{
		{
			name:       "balance cannot go negative",
			sql:        `UPDATE wallets SET balance = balance - 200 WHERE id = 'w_c1'`,
			wantCode:   "23514", // check_violation
			wantConstr: "wallets_balance_non_negative",
		},
		{
			name:       "transfer amount must be positive",
			sql:        `INSERT INTO transfers (from_wallet_id, to_wallet_id, amount) VALUES ('w_c1', 'w_c2', 0)`,
			wantCode:   "23514",
			wantConstr: "transfers_amount_positive",
		},
		{
			name:       "source and destination must differ",
			sql:        `INSERT INTO transfers (from_wallet_id, to_wallet_id, amount) VALUES ('w_c1', 'w_c1', 10)`,
			wantCode:   "23514",
			wantConstr: "transfers_distinct_wallets",
		},
		{
			name:       "failure_reason must agree with status",
			sql:        `INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, status, failure_reason) VALUES ('w_c1', 'w_c2', 10, 'PENDING', 'INSUFFICIENT_FUNDS')`,
			wantCode:   "23514",
			wantConstr: "transfers_failure_reason_consistent",
		},
		{
			name:       "ledger entry must reference a matching transfer",
			sql:        `INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount) VALUES (gen_random_uuid(), 'w_c1', 'DEBIT', 10)`,
			wantCode:   "23503", // foreign_key_violation
			wantConstr: "ledger_transfer_amount_fk",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.Pool.Exec(ctx, tc.sql)
			if err == nil {
				t.Fatal("statement must be rejected by a constraint")
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("want a PgError, got %v", err)
			}
			if pgErr.Code != tc.wantCode || pgErr.ConstraintName != tc.wantConstr {
				t.Fatalf("got SQLSTATE %s constraint %q, want %s / %s",
					pgErr.Code, pgErr.ConstraintName, tc.wantCode, tc.wantConstr)
			}
		})
	}

	// None of the rejected statements may have left a trace.
	if n := env.CountRows(t, "SELECT count(*) FROM transfers"); n != 0 {
		t.Errorf("transfers rows = %d, want 0", n)
	}
	if n := env.CountRows(t, "SELECT count(*) FROM ledger_entries"); n != 0 {
		t.Errorf("ledger_entries rows = %d, want 0", n)
	}
	if got := env.Balance(t, "w_c1"); got != 100 {
		t.Errorf("w_c1 balance = %d, want 100", got)
	}
}
