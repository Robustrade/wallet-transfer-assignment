package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-service/internal/database"
	"wallet-service/internal/domain"
	"wallet-service/internal/repository"
	"wallet-service/internal/testutil"
)

func begin(t *testing.T, pool *pgxpool.Pool) (pgx.Tx, *repository.Repository) {
	t.Helper()
	tx, err := database.BeginReadCommitted(t.Context(), pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn := tx.Conn()
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) && !conn.IsClosed() {
			t.Error(err)
		}
	})
	return tx, repository.New(tx)
}

var payload = domain.Payload{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

func TestRepositoryCommitAndReplay(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	ctx := t.Context()
	tx, repo := begin(t, pool)
	var isolation string
	if err := tx.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil || isolation != "read committed" {
		t.Fatalf("isolation %q: %v", isolation, err)
	}
	if record, err := repo.ClaimOrLoad(ctx, "key", payload); err != nil || record != nil {
		t.Fatalf("claim: %v, %v", record, err)
	}
	from, to, err := repo.LockWallets(ctx, payload.FromWalletID, payload.ToWalletID)
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := repo.CreatePending(ctx, payload, from.Currency)
	if err != nil {
		t.Fatal(err)
	}
	from.Balance -= payload.Amount
	to.Balance += payload.Amount
	if err := repo.UpdateBalances(ctx, from, to); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertLedgerPair(ctx, transfer.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteTransfer(ctx, transfer.ID, domain.Processed, ""); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("{\"transferId\":%q,\"status\":\"PROCESSED\"}\n", transfer.ID)
	result := repository.Result{TransferID: transfer.ID, HTTPStatus: 201, Body: body}
	if err := repo.SaveResult(ctx, "key", result); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	_, replay := begin(t, pool)
	record, err := replay.ClaimOrLoad(ctx, "key", payload)
	if err != nil || record == nil {
		t.Fatalf("replay: %v, %v", record, err)
	}
	if record.Payload != payload || record.Result != result {
		t.Fatalf("unexpected stored result: %+v", record)
	}
	changed := payload
	changed.Amount++
	record, err = replay.ClaimOrLoad(ctx, "key", changed)
	if err != nil || record.Payload != payload {
		t.Fatalf("original identity changed: %+v %v", record, err)
	}
	var source, destination, entries, net int64
	if err := pool.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet_1'").Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet_2'").Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*), sum(CASE entry_type WHEN 'DEBIT' THEN -amount_minor ELSE amount_minor END) FROM ledger_entries").Scan(&entries, &net); err != nil {
		t.Fatal(err)
	}
	if source != 400 || destination != 300 || entries != 2 || net != 0 {
		t.Fatalf("balances=%d/%d, entries=%d, net=%d", source, destination, entries, net)
	}
	var matching int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transfer_id=$1 AND amount_minor=100
		AND ((wallet_id='wallet_1' AND entry_type='DEBIT') OR (wallet_id='wallet_2' AND entry_type='CREDIT'))`, transfer.ID).Scan(&matching); err != nil || matching != 2 {
		t.Fatalf("ledger does not match transfer: %d entries, %v", matching, err)
	}
	if err := replay.SaveResult(ctx, "key", result); !errors.Is(err, repository.ErrInvariant) {
		t.Fatalf("result overwritten: %v", err)
	}
	if err := replay.CompleteTransfer(ctx, transfer.ID, domain.Failed, string(domain.ErrInsufficientFunds)); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("terminal transition: %v", err)
	}
}

func TestRepositoryRollback(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	ctx := t.Context()
	tx, repo := begin(t, pool)
	if _, err := repo.ClaimOrLoad(ctx, "rollback", payload); err != nil {
		t.Fatal(err)
	}
	from, to, err := repo.LockWallets(ctx, "wallet_1", "wallet_2")
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := repo.CreatePending(ctx, payload, "INR")
	if err != nil {
		t.Fatal(err)
	}
	from.Balance -= 100
	to.Balance += 100
	if err := repo.UpdateBalances(ctx, from, to); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertLedgerPair(ctx, transfer.ID); err != nil {
		t.Fatal(err)
	}
	// A second pair violates uniqueness and aborts the transaction.
	assertSQLState(t, repo.InsertLedgerPair(ctx, transfer.ID), "23505")
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var balance, count int
	if err := pool.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet_1'").Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM transfers)+(SELECT count(*) FROM ledger_entries)+(SELECT count(*) FROM idempotency_records)").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if balance != 500 || count != 0 {
		t.Fatalf("partial writes: balance=%d rows=%d", balance, count)
	}
	_, retry := begin(t, pool)
	if record, err := retry.ClaimOrLoad(ctx, "rollback", payload); err != nil || record != nil {
		t.Fatalf("retry: %v %v", record, err)
	}
}

func TestRepositoryFailures(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	ctx := t.Context()
	tx, repo := begin(t, pool)
	if _, _, err := repo.LockWallets(ctx, "wallet_1", "missing"); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatal(err)
	}
	if _, _, err := repo.LockWallets(ctx, "wallet_1", "wallet_1"); !errors.Is(err, domain.ErrSelfTransfer) {
		t.Fatal(err)
	}
	if _, err := repo.ClaimOrLoad(ctx, "failed", payload); err != nil {
		t.Fatal(err)
	}
	transfer, err := repo.CreatePending(ctx, payload, "INR")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteTransfer(ctx, transfer.ID, domain.Failed, ""); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal(err)
	}
	if err := repo.CompleteTransfer(ctx, transfer.ID, domain.Failed, string(domain.ErrInsufficientFunds)); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertLedgerPair(ctx, transfer.ID); !errors.Is(err, repository.ErrInvariant) {
		t.Fatalf("ledger for failed transfer: %v", err)
	}
	result := repository.Result{TransferID: transfer.ID, HTTPStatus: 422, Body: `{"error":{"code":"INSUFFICIENT_FUNDS"}}`}
	if err := repo.SaveResult(ctx, "failed", result); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimOrLoad(ctx, "missing", payload); err != nil {
		t.Fatal(err)
	}
	missing := repository.Result{HTTPStatus: 404, Body: `{"error":{"code":"WALLET_NOT_FOUND"}}`}
	if err := repo.SaveResult(ctx, "missing", missing); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, replay := begin(t, pool)
	for key, want := range map[string]repository.Result{"failed": result, "missing": missing} {
		record, err := replay.ClaimOrLoad(ctx, key, payload)
		if err != nil || record == nil || record.Result != want {
			t.Fatalf("%s: %v %v", key, record, err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM ledger_entries").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed ledger count %d: %v", count, err)
	}
}

func TestRepositoryRejectsIncompleteRecordsAndMissingUpdates(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	ctx := t.Context()
	tx, repo := begin(t, pool)
	if _, err := repo.ClaimOrLoad(ctx, "incomplete", payload); err != nil {
		t.Fatal(err)
	}
	// The schema permits this; readers must report corruption, not replay NULLs.
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, next := begin(t, pool)
	if _, err := next.ClaimOrLoad(ctx, "incomplete", payload); !errors.Is(err, repository.ErrInvariant) {
		t.Fatal(err)
	}
	if err := next.SaveResult(ctx, "absent", repository.Result{HTTPStatus: 404, Body: `{}`}); !errors.Is(err, repository.ErrInvariant) {
		t.Fatal(err)
	}
	if err := next.SaveResult(ctx, "incomplete", repository.Result{HTTPStatus: 201, Body: "invalid"}); !errors.Is(err, repository.ErrInvariant) {
		t.Fatal(err)
	}
	if err := next.UpdateBalances(ctx, domain.Wallet{ID: "missing"}, domain.Wallet{ID: "also_missing"}); !errors.Is(err, repository.ErrInvariant) {
		t.Fatal(err)
	}
	if err := next.CompleteTransfer(ctx, "00000000-0000-0000-0000-000000000000", domain.Processed, ""); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal(err)
	}
}

// Observe the database wait instead of relying on goroutine scheduling or sleeps.
func awaitLock(t *testing.T, pool *pgxpool.Pool, pid uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, "SELECT coalesce(wait_event_type='Lock', false) FROM pg_stat_activity WHERE pid=$1", int64(pid)).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestConcurrentKeyClaim(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(fmt.Sprintf("commit_%t", commit), func(t *testing.T) {
			pool, _ := testutil.NewDatabase(t)
			ctx := t.Context()
			ownerTx, owner := begin(t, pool)
			if _, err := owner.ClaimOrLoad(ctx, "same", payload); err != nil {
				t.Fatal(err)
			}
			waitTx, waiter := begin(t, pool)
			type outcome struct {
				record *repository.Record
				err    error
			}
			done := make(chan outcome, 1)
			waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer func() {
				cancel()
				for range done {
				}
			}()
			pid := waitTx.Conn().PgConn().PID()
			go func() {
				defer close(done)
				record, err := waiter.ClaimOrLoad(waitCtx, "same", payload)
				done <- outcome{record, err}
			}()
			awaitLock(t, pool, pid)
			if commit {
				if err := owner.SaveResult(ctx, "same", repository.Result{HTTPStatus: 404, Body: `{"error":{"code":"WALLET_NOT_FOUND"}}`}); err != nil {
					t.Fatal(err)
				}
				if err := ownerTx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			} else if err := ownerTx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			result := <-done
			if result.err != nil || (result.record != nil) != commit {
				t.Fatalf("waiter: %+v", result)
			}
		})
	}
}

func TestWalletLockOrderAndCancellation(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	ctx := t.Context()
	ownerTx, owner := begin(t, pool)
	from, to, err := owner.LockWallets(ctx, "wallet_2", "wallet_1")
	if err != nil || from.ID != "wallet_2" || to.ID != "wallet_1" {
		t.Fatalf("reversed wallets: %v %v %v", from, to, err)
	}
	waitTx, waiter := begin(t, pool)
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	done := make(chan error, 1)
	defer func() {
		cancel()
		for range done {
		}
	}()
	pid := waitTx.Conn().PgConn().PID()
	go func() {
		defer close(done)
		_, _, err := waiter.LockWallets(waitCtx, "wallet_1", "wallet_2")
		done <- err
	}()
	awaitLock(t, pool, pid)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lock: %v", err)
	}
	if err := ownerTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestReversedRequestLocksLowerWalletFirst(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	ctx := t.Context()
	blockerTx, _ := begin(t, pool)
	if _, err := blockerTx.Exec(ctx, "SELECT id FROM wallets WHERE id='wallet_1' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	waitTx, waiter := begin(t, pool)
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	done := make(chan error, 1)
	defer func() {
		cancel()
		for range done {
		}
	}()
	pid := waitTx.Conn().PgConn().PID()
	go func() {
		defer close(done)
		_, _, err := waiter.LockWallets(waitCtx, "wallet_2", "wallet_1")
		done <- err
	}()
	awaitLock(t, pool, pid)
	// If the waiter had locked its source first, this NOWAIT would fail.
	probeTx, _ := begin(t, pool)
	if _, err := probeTx.Exec(ctx, "SELECT id FROM wallets WHERE id='wallet_2' FOR UPDATE NOWAIT"); err != nil {
		t.Fatal(err)
	}
	if err := probeTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := blockerTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func assertSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("wanted SQLSTATE %s, got %v", code, err)
	}
}

func TestMigrationsAndConstraints(t *testing.T) {
	pool, m := testutil.NewDatabase(t)
	ctx := t.Context()
	if err := m.Up(); !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	seed, err := os.ReadFile("../../seeds/wallets.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE wallets SET balance_minor=400 WHERE id='wallet_1'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	var balance int
	if err := pool.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet_1'").Scan(&balance); err != nil || balance != 400 {
		t.Fatalf("seed reset balance: %d %v", balance, err)
	}
	var id string
	if err := pool.QueryRow(ctx, "INSERT INTO transfers(from_wallet_id,to_wallet_id,currency_code,amount_minor) VALUES ('wallet_1','wallet_2','INR',100) RETURNING id::text").Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, sql, code string }{
		{"negative balance", "UPDATE wallets SET balance_minor=-1", "23514"},
		{"currency format", "UPDATE wallets SET currency_code='inr'", "23514"},
		{"duplicate wallet", "INSERT INTO wallets(id,currency_code) VALUES ('wallet_1','INR')", "23505"},
		{"zero amount", "UPDATE transfers SET amount_minor=0", "23514"},
		{"invalid status", "UPDATE transfers SET status='UNKNOWN'", "23514"},
		{"completion required", "UPDATE transfers SET status='PROCESSED'", "23514"},
		{"failure code required", "UPDATE transfers SET status='FAILED',completed_at=now()", "23514"},
		{"self transfer", "UPDATE transfers SET to_wallet_id=from_wallet_id", "23514"},
		{"missing wallet", "UPDATE transfers SET to_wallet_id='absent'", "23503"},
		{"currency mismatch", "UPDATE transfers SET currency_code='USD'", "23503"},
		{"referenced wallet deletion", "DELETE FROM wallets WHERE id='wallet_1'", "23001"},
		{"orphan ledger", "INSERT INTO ledger_entries(transfer_id,wallet_id,entry_type,amount_minor) VALUES ('00000000-0000-0000-0000-000000000000','wallet_1','DEBIT',1)", "23503"},
		{"invalid ledger type", fmt.Sprintf("INSERT INTO ledger_entries(transfer_id,wallet_id,entry_type,amount_minor) VALUES ('%s','wallet_1','OTHER',1)", id), "23514"},
		{"negative ledger amount", fmt.Sprintf("INSERT INTO ledger_entries(transfer_id,wallet_id,entry_type,amount_minor) VALUES ('%s','wallet_1','DEBIT',-1)", id), "23514"},
		{"empty key", "INSERT INTO idempotency_records(idempotency_key,request_payload) VALUES ('','{}')", "23514"},
		{"long key", "INSERT INTO idempotency_records(idempotency_key,request_payload) VALUES (repeat('x',129),'{}')", "23514"},
		{"half response", "INSERT INTO idempotency_records(idempotency_key,request_payload,http_status) VALUES ('half','{}',201)", "23514"},
		{"invalid http status", "INSERT INTO idempotency_records(idempotency_key,request_payload,http_status,response_body) VALUES ('bad','{}',600,'{}')", "23514"},
	} {
		t.Run(tc.name, func(t *testing.T) { _, err := pool.Exec(ctx, tc.sql); assertSQLState(t, err, tc.code) })
	}
	if _, err := pool.Exec(ctx, "INSERT INTO idempotency_records(idempotency_key,request_payload) VALUES ('duplicate','{}')"); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, "INSERT INTO idempotency_records(idempotency_key,request_payload) VALUES ('duplicate','{}')")
	assertSQLState(t, err, "23505")
	if err := m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON c.relnamespace=n.oid WHERE n.nspname='public' AND c.relname <> 'schema_migrations'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	// migrate's bookkeeping table also has its own primary-key index.
	if count != 1 {
		t.Fatalf("schema objects remain after down: %d", count)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedMigrationIsAtomic(t *testing.T) {
	pool, m := testutil.NewDatabase(t)
	ctx := t.Context()
	if err := m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	// A conflict near the end must roll back tables created earlier in the file.
	if _, err := pool.Exec(ctx, "CREATE TABLE idempotency_records (sentinel boolean)"); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err == nil {
		t.Fatal("expected migration failure")
	}
	_, dirty, err := m.Version()
	if err != nil || !dirty {
		t.Fatalf("dirty=%t: %v", dirty, err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('wallets') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("partial schema remains: %t %v", exists, err)
	}
}

func TestDatabaseOpenErrors(t *testing.T) {
	if _, err := database.Open(t.Context(), "postgres://user:secret@localhost:bad/db"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe config error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := database.Open(ctx, "postgres://localhost:1/nonexistent?sslmode=disable"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled connection: %v", err)
	}
}
