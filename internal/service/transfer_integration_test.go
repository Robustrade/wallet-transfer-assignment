package service_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/repository"
	"wallettransfer/internal/service"
)

// These are real PostgreSQL integration tests, not mocks. Each subtest gets
// a random schema, the production migration, and fresh seed wallets.
// Unlike the optional migration tests, a missing test database is an error.
func TestPostgresTransferService(t *testing.T) {
	t.Run("successful transfer", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		svc := service.New(repository.NewPostgres(db))

		result, err := svc.Transfer(context.Background(), domain.TransferRequest{
			IdempotencyKey: "success-001", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		})
		if err != nil {
			t.Fatalf("Transfer: %v", err)
		}
		if result.ID == "" || result.Status != domain.StatusProcessed || result.FailureReason != "" {
			t.Fatalf("unexpected transfer outcome: %+v", result)
		}
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertTransferRecord(t, db, result.ID, "PROCESSED", "")
		assertLedger(t, db, result.ID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertReconciles(t, db)
		assertTransferCount(t, db, 1)
	})

	t.Run("insufficient funds commits failure without money movement", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 50, "B": 200, "reserve": 1000})
		svc := service.New(repository.NewPostgres(db))

		result, err := svc.Transfer(context.Background(), domain.TransferRequest{
			IdempotencyKey: "failed-001", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		})
		if err != nil {
			t.Fatalf("Transfer: %v", err)
		}
		if result.ID == "" || result.Status != domain.StatusFailed || result.FailureReason != domain.ReasonInsufficientFunds {
			t.Fatalf("unexpected failure result: %+v", result)
		}
		assertBalance(t, db, "A", 50)
		assertBalance(t, db, "B", 200)
		assertBalance(t, db, "reserve", 1000)
		assertTransferRecord(t, db, result.ID, "FAILED", domain.ReasonInsufficientFunds)
		assertLedger(t, db, result.ID, nil)
		assertReconciles(t, db)
		assertTransferCount(t, db, 1)
	})
}

type ledgerExpectation struct {
	wallet, kind string
	amount       int64
}

func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WALLET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("WALLET_TEST_DATABASE_URL is required: PostgreSQL tests must run, not skip")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		t.Fatalf("psql must be on PATH: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatalf("WALLET_TEST_DATABASE_URL must be a postgres:// or postgresql:// URL: %v", err)
	}
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatal(err)
	}
	schema := "transfer_test_" + hex.EncodeToString(bytes)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	adminDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if _, err := adminDB.ExecContext(dropCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop test schema %s: %v", schema, err)
		}
	})
	_, filename, _, _ := runtime.Caller(0)
	migration := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "migrations", "000001_init.up.sql"))
	cmd := exec.Command("psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "--dbname="+dsn, "-f", migration)
	cmd.Env = append(os.Environ(), "PGOPTIONS=-c search_path="+schema)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply real migration: %v\n%s", err, out)
	}

	isolatedDSN := *parsed
	q := isolatedDSN.Query()
	q.Set("search_path", schema)
	// Tag every connection so concurrency tests can prove real PG lock waits.
	q.Set("application_name", "wallet_concurrency_"+schema)
	isolatedDSN.RawQuery = q.Encode()
	db, err := sql.Open("postgres", isolatedDSN.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connect with isolated search_path: %v", err)
	}
	var current string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&current); err != nil || current != schema {
		t.Fatalf("unsafe database search_path: current_schema()=%q, err=%v, expected %q", current, err, schema)
	}
	return db
}

func seedWallets(t *testing.T, db *sql.DB, wallets map[string]int64) {
	t.Helper()
	for id, amount := range wallets {
		if _, err := db.Exec("INSERT INTO wallets(id, opening_balance, balance) VALUES($1,$2,$2)", id, amount); err != nil {
			t.Fatalf("seed wallet %s: %v", id, err)
		}
	}
}

func assertBalance(t *testing.T, db *sql.DB, id string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow("SELECT balance FROM wallets WHERE id=$1", id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("wallet %s balance=%d; want %d", id, got, want)
	}
}

func assertTransferRecord(t *testing.T, db *sql.DB, id, wantStatus, wantReason string) {
	t.Helper()
	var status string
	var reason sql.NullString
	if err := db.QueryRow("SELECT status, failure_reason FROM transfers WHERE id=$1", id).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || reason.String != wantReason || (reason.Valid != (wantReason != "")) {
		t.Errorf("transfer %s status=%s reason=%+v; want status=%s reason=%q", id, status, reason, wantStatus, wantReason)
	}
}

func assertLedger(t *testing.T, db *sql.DB, transferID string, expected []ledgerExpectation) {
	t.Helper()
	rows, err := db.Query("SELECT wallet_id, entry_type, amount FROM ledger_entries WHERE transfer_id=$1 ORDER BY entry_type", transferID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actual []ledgerExpectation
	for rows.Next() {
		var e ledgerExpectation
		if err := rows.Scan(&e.wallet, &e.kind, &e.amount); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(expected) {
		t.Fatalf("transfer %s ledger count=%d; want %d; got %+v", transferID, len(actual), len(expected), actual)
	}
	for _, want := range expected {
		found := false
		for _, got := range actual {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing ledger entry %+v; actual %+v", want, actual)
		}
	}
}

func assertReconciles(t *testing.T, db *sql.DB) {
	t.Helper()
	const query = `SELECT w.id, w.balance, w.opening_balance + COALESCE((
        SELECT SUM(CASE WHEN e.entry_type='CREDIT' THEN e.amount ELSE -e.amount END)
        FROM ledger_entries e WHERE e.wallet_id=w.id
    ),0) AS calculated FROM wallets w`
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var wallet string
		var stored, calculated int64
		if err := rows.Scan(&wallet, &stored, &calculated); err != nil {
			t.Fatal(err)
		}
		if stored != calculated {
			problems = append(problems, fmt.Sprintf("%s: stored=%d calculated=%d", wallet, stored, calculated))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Errorf("reconciliation failed: %s", strings.Join(problems, "; "))
	}
}

func assertTransferCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM transfers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("transfer count=%d; want %d", count, want)
	}
}

// Increment 3 deliberately exercises *persisted* idempotency outcomes, not
// just return values. These tests are sequential; concurrent duplicates are
// reserved for the next increment.
func TestPostgresIdempotencyAndRollback(t *testing.T) {
	t.Run("repeated successful transfer returns stored result", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		svc := service.New(repository.NewPostgres(db))
		req := domain.TransferRequest{
			IdempotencyKey: "replay-success-001", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		}
		first, err := svc.Transfer(context.Background(), req)
		if err != nil {
			t.Fatalf("first transfer: %v", err)
		}
		second, err := svc.Transfer(context.Background(), req)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if first.ID == "" || first != second || first.Status != domain.StatusProcessed {
			t.Fatalf("initial=%+v, replay=%+v; expected identical processed results", first, second)
		}
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertStoredTransfer(t, db, first.ID, req, domain.StatusProcessed, "")
		assertLedger(t, db, first.ID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})

	t.Run("different payload conflicts before validating nonexistent wallet", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200, "C": 50})
		svc := service.New(repository.NewPostgres(db))
		original := domain.TransferRequest{
			IdempotencyKey: "same-key-different-operation", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		}
		first, err := svc.Transfer(context.Background(), original)
		if err != nil {
			t.Fatalf("original transfer: %v", err)
		}
		changes := []struct {
			name string
			req  domain.TransferRequest
		}{
			{"changed amount", domain.TransferRequest{IdempotencyKey: "same-key-different-operation", FromWalletID: "A", ToWalletID: "B", Amount: 101}},
			{"changed source", domain.TransferRequest{IdempotencyKey: "same-key-different-operation", FromWalletID: "C", ToWalletID: "B", Amount: 100}},
			{"nonexistent destination", domain.TransferRequest{IdempotencyKey: "same-key-different-operation", FromWalletID: "A", ToWalletID: "missing-wallet", Amount: 100}},
		}
		for _, change := range changes {
			t.Run(change.name, func(t *testing.T) {
				result, err := svc.Transfer(context.Background(), change.req)
				if !errors.Is(err, domain.ErrIdempotencyConflict) {
					t.Fatalf("Transfer(%+v) error=%v, want ErrIdempotencyConflict", change.req, err)
				}
				if result != (domain.TransferResult{}) {
					t.Fatalf("conflicting request returned a transfer result: %+v", result)
				}
				assertBalance(t, db, "A", 400)
				assertBalance(t, db, "B", 300)
				assertBalance(t, db, "C", 50)
				assertStoredTransfer(t, db, first.ID, original, domain.StatusProcessed, "")
				assertLedger(t, db, first.ID, []ledgerExpectation{
					{wallet: "A", kind: "DEBIT", amount: 100},
					{wallet: "B", kind: "CREDIT", amount: 100},
				})
				assertTransferCount(t, db, 1)
				assertTotalLedgerCount(t, db, 2)
				assertReconciles(t, db)
			})
		}
	})

	t.Run("failed transfer stays failed after funding via another transfer", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 50, "B": 200, "reserve": 1000})
		svc := service.New(repository.NewPostgres(db))
		failedReq := domain.TransferRequest{
			IdempotencyKey: "failed-then-funded", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		}
		failed, err := svc.Transfer(context.Background(), failedReq)
		if err != nil {
			t.Fatalf("initial failed transfer: %v", err)
		}
		if failed.ID == "" || failed.Status != domain.StatusFailed || failed.FailureReason != domain.ReasonInsufficientFunds {
			t.Fatalf("unexpected initial failure: %+v", failed)
		}
		assertLedger(t, db, failed.ID, nil)

		// Replenish A exclusively through the public transfer service. Updating
		// wallets.balance directly would break our ledger reconciliation invariant.
		fundReq := domain.TransferRequest{
			IdempotencyKey: "fund-from-reserve", FromWalletID: "reserve", ToWalletID: "A", Amount: 200,
		}
		funded, err := svc.Transfer(context.Background(), fundReq)
		if err != nil || funded.Status != domain.StatusProcessed {
			t.Fatalf("funding transfer: result=%+v err=%v", funded, err)
		}
		assertBalance(t, db, "A", 250)
		assertBalance(t, db, "B", 200)
		assertBalance(t, db, "reserve", 800)

		replayed, err := svc.Transfer(context.Background(), failedReq)
		if err != nil {
			t.Fatalf("replay failed transfer: %v", err)
		}
		if replayed != failed {
			t.Fatalf("old failed request changed: first=%+v replay=%+v", failed, replayed)
		}
		assertBalance(t, db, "A", 250)
		assertBalance(t, db, "B", 200)
		assertBalance(t, db, "reserve", 800)
		assertStoredTransfer(t, db, failed.ID, failedReq, domain.StatusFailed, domain.ReasonInsufficientFunds)
		assertStoredTransfer(t, db, funded.ID, fundReq, domain.StatusProcessed, "")
		assertLedger(t, db, failed.ID, nil)
		assertLedger(t, db, funded.ID, []ledgerExpectation{
			{wallet: "reserve", kind: "DEBIT", amount: 200},
			{wallet: "A", kind: "CREDIT", amount: 200},
		})
		assertTransferCount(t, db, 2)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})

	t.Run("database error after financial writes rolls back claim balances and ledger", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		svc := service.New(repository.NewPostgres(db))
		req := domain.TransferRequest{
			IdempotencyKey: "rollback-then-retry", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		}

		// This trigger fires at the LAST write in the business workflow, after
		// debit, credit and paired ledger insertion have executed in the same tx.
		// Only the isolated test schema contains this failure injection.
		const faultSQL = `CREATE FUNCTION reject_test_finalization() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'PENDING' AND NEW.status = 'PROCESSED' THEN
        -- Prove the error is injected AFTER the financial SQL statements,
        -- rather than before they have executed.
        IF (SELECT balance FROM wallets WHERE id='A') IS DISTINCT FROM 400
           OR (SELECT balance FROM wallets WHERE id='B') IS DISTINCT FROM 300
           OR (SELECT count(*) FROM ledger_entries WHERE transfer_id=NEW.id) <> 2 THEN
            RAISE EXCEPTION 'test-only financial mutations not completed before finalization';
        END IF;
        RAISE EXCEPTION 'test-only forced finalization error';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER reject_test_finalization BEFORE UPDATE OF status ON transfers
FOR EACH ROW EXECUTE FUNCTION reject_test_finalization();`
		if _, err := db.Exec(faultSQL); err != nil {
			t.Fatalf("install test-only failure injection: %v", err)
		}

		result, err := svc.Transfer(context.Background(), req)
		if err == nil {
			t.Fatal("transfer unexpectedly succeeded despite finalization trigger")
		}
		if result != (domain.TransferResult{}) {
			t.Fatalf("database error returned a financial result: %+v", result)
		}
		if !strings.Contains(err.Error(), "test-only forced finalization error") {
			t.Fatalf("unexpected database error: %v", err)
		}
		assertBalance(t, db, "A", 500)
		assertBalance(t, db, "B", 200)
		assertTransferCount(t, db, 0)
		assertTransferKeyCount(t, db, req.IdempotencyKey, 0)
		assertTotalLedgerCount(t, db, 0)
		assertReconciles(t, db)

		if _, err := db.Exec("DROP TRIGGER reject_test_finalization ON transfers"); err != nil {
			t.Fatalf("remove test trigger: %v", err)
		}
		if _, err := db.Exec("DROP FUNCTION reject_test_finalization()"); err != nil {
			t.Fatalf("remove test function: %v", err)
		}
		// The failed database transaction did not permanently consume the key.
		recovered, err := svc.Transfer(context.Background(), req)
		if err != nil || recovered.ID == "" || recovered.Status != domain.StatusProcessed {
			t.Fatalf("retry after rollback: result=%+v err=%v", recovered, err)
		}
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertStoredTransfer(t, db, recovered.ID, req, domain.StatusProcessed, "")
		assertLedger(t, db, recovered.ID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertTransferCount(t, db, 1)
		assertTransferKeyCount(t, db, req.IdempotencyKey, 1)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})

	t.Run("lost response after commit can be reconstructed by fresh service", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		req := domain.TransferRequest{
			IdempotencyKey: "lost-response-replay", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		}

		// Ignore the first successful result: model a client whose response
		// never arrived, although PostgreSQL committed the transfer.
		if _, err := service.New(repository.NewPostgres(db)).Transfer(context.Background(), req); err != nil {
			t.Fatalf("original request: %v", err)
		}
		var durableID, durableStatus string
		var durableReason sql.NullString
		if err := db.QueryRow(`SELECT id::text, status, failure_reason FROM transfers
            WHERE idempotency_key=$1`, req.IdempotencyKey).Scan(&durableID, &durableStatus, &durableReason); err != nil {
			t.Fatalf("read durable outcome: %v", err)
		}
		if durableStatus != domain.StatusProcessed || durableReason.Valid {
			t.Fatalf("wrong durable outcome: %s, reason=%+v", durableStatus, durableReason)
		}

		// A fresh service/repository object has no local memory of the request.
		restarted := service.New(repository.NewPostgres(db))
		replay, err := restarted.Transfer(context.Background(), req)
		if err != nil {
			t.Fatalf("retry after lost response: %v", err)
		}
		if replay != (domain.TransferResult{ID: durableID, Status: domain.StatusProcessed}) {
			t.Fatalf("replay=%+v, durable transfer ID=%s", replay, durableID)
		}
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertStoredTransfer(t, db, durableID, req, domain.StatusProcessed, "")
		assertLedger(t, db, durableID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})
}

// assertStoredTransfer checks the immutable request identity as well as its
// terminal status. A result-only assertion would miss accidental row mutation.
func assertStoredTransfer(t *testing.T, db *sql.DB, id string, req domain.TransferRequest, wantStatus, wantReason string) {
	t.Helper()
	var gotID, key, from, to, status string
	var amount int64
	var reason sql.NullString
	err := db.QueryRow(`SELECT id::text, idempotency_key, from_wallet_id, to_wallet_id,
        amount, status, failure_reason FROM transfers WHERE id=$1`, id).Scan(
		&gotID, &key, &from, &to, &amount, &status, &reason,
	)
	if err != nil {
		t.Fatalf("read transfer %s: %v", id, err)
	}
	if gotID != id || key != req.IdempotencyKey || from != req.FromWalletID || to != req.ToWalletID ||
		amount != req.Amount || status != wantStatus || reason.String != wantReason || reason.Valid != (wantReason != "") {
		t.Errorf("persisted transfer mismatch: id=%s key=%s from=%s to=%s amount=%d status=%s reason=%+v; want req=%+v status=%s reason=%q",
			gotID, key, from, to, amount, status, reason, req, wantStatus, wantReason)
	}
}

func assertTotalLedgerCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM ledger_entries").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("total ledger entry count=%d; want %d", count, want)
	}
}

func assertTransferKeyCount(t *testing.T, db *sql.DB, key string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM transfers WHERE idempotency_key=$1", key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("transfers for key %q = %d; want %d", key, count, want)
	}
}
