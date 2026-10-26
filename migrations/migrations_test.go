package migrations_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This test creates and drops a dedicated randomized PostgreSQL schema.
// It requires psql and WALLET_TEST_DATABASE_URL; it never changes public.
func TestPostgresMigrationContract(t *testing.T) {
	dsn := os.Getenv("WALLET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set WALLET_TEST_DATABASE_URL to run PostgreSQL schema tests")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		t.Skip("psql is required for PostgreSQL schema tests")
	}

	nameBytes := make([]byte, 8)
	if _, err := rand.Read(nameBytes); err != nil {
		t.Fatal(err)
	}
	schema := "wallet_test_" + hex.EncodeToString(nameBytes)
	_, filename, _, _ := runtime.Caller(0)
	migration := filepath.Join(filepath.Dir(filename), "000001_init.up.sql")

	psql := func(useSchema bool, args ...string) (string, error) {
		cmd := exec.Command("psql", append([]string{"-X", "-q", "-v", "ON_ERROR_STOP=1", "--dbname=" + dsn}, args...)...)
		if useSchema {
			cmd.Env = append(os.Environ(), "PGOPTIONS=-c search_path="+schema)
		}
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	run := func(sql string) string {
		t.Helper()
		out, err := psql(true, "-t", "-A", "-c", sql)
		if err != nil {
			t.Fatalf("SQL failed: %s: %v\n%s", sql, err, out)
		}
		return out
	}

	if out, err := psql(false, "-c", "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create test schema: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := psql(false, "-c", "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v\n%s", err, out)
		}
	})
	if out, err := psql(true, "-f", migration); err != nil {
		t.Fatalf("apply migration: %v\n%s", err, out)
	}

	run(`INSERT INTO wallets(id, opening_balance, balance)
         VALUES ('A', 50, 50), ('B', 0, 0), ('reserve', 1000, 1000)`)

	// Simulate an atomic financial posting solely to exercise schema and
	// reconciliation. Transfer execution tests come in the next increment.
	run(`BEGIN;
INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
VALUES ('11111111-1111-4111-8111-111111111111', 'key-1', 'reserve', 'A', 200, 'PROCESSED');
UPDATE wallets SET balance = balance - 200 WHERE id = 'reserve';
UPDATE wallets SET balance = balance + 200 WHERE id = 'A';
INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount) VALUES
('11111111-1111-4111-8111-111111111111', 'reserve', 'DEBIT', 200),
('11111111-1111-4111-8111-111111111111', 'A', 'CREDIT', 200);
COMMIT;`)

	t.Run("reconciliation", func(t *testing.T) {
		actual := run(`SELECT count(*) FROM wallets w WHERE w.balance <> w.opening_balance +
            COALESCE((SELECT SUM(CASE WHEN e.entry_type = 'CREDIT' THEN e.amount ELSE -e.amount END)
                      FROM ledger_entries e WHERE e.wallet_id = w.id), 0)`)
		if actual != "0" {
			t.Fatalf("wallet reconciliation discrepancies: %s", actual)
		}
	})

	t.Run("conflicting key takes priority over missing wallet foreign key", func(t *testing.T) {
		// The unique key already exists. ON CONFLICT DO NOTHING must not
		// try to create a new transfer referencing the absent wallet.
		actual := run(`INSERT INTO transfers(id, idempotency_key, from_wallet_id,
                to_wallet_id, amount, status)
                VALUES ('22222222-2222-4222-8222-222222222222', 'key-1', 'reserve',
                        'does-not-exist', 200, 'PENDING')
                ON CONFLICT (idempotency_key) DO NOTHING RETURNING id`)
		if actual != "" {
			t.Fatalf("conflicting key inserted a transfer unexpectedly: %s", actual)
		}
	})

	checkRejected := func(label, statement, errorFragment string) {
		t.Run(label, func(t *testing.T) {
			out, err := psql(true, "-c", statement)
			if err == nil {
				t.Fatalf("expected SQL constraint rejection; got success: %s", out)
			}
			if !strings.Contains(strings.ToLower(out), strings.ToLower(errorFragment)) {
				t.Fatalf("expected error containing %q; got: %v\n%s", errorFragment, err, out)
			}
		})
	}
	checkRejected("negative balance", `UPDATE wallets SET balance = -1 WHERE id = 'A'`, "check constraint")
	checkRejected("immutable opening balance", `UPDATE wallets SET opening_balance = 999 WHERE id = 'A'`, "immutable")
	checkRejected("zero transfer amount", `INSERT INTO transfers(id,idempotency_key,from_wallet_id,to_wallet_id,amount,status)
        VALUES ('33333333-3333-4333-8333-333333333333','key-2','A','B',0,'PENDING')`, "check constraint")
	checkRejected("same source and destination", `INSERT INTO transfers(id,idempotency_key,from_wallet_id,to_wallet_id,amount,status)
        VALUES ('33333333-3333-4333-8333-333333333333','key-2','A','A',10,'PENDING')`, "check constraint")
	checkRejected("missing foreign key", `INSERT INTO transfers(id,idempotency_key,from_wallet_id,to_wallet_id,amount,status)
        VALUES ('33333333-3333-4333-8333-333333333333','key-2','A','absent',10,'PENDING')`, "foreign key constraint")
	checkRejected("duplicate idempotency key", `INSERT INTO transfers(id,idempotency_key,from_wallet_id,to_wallet_id,amount,status)
        VALUES ('33333333-3333-4333-8333-333333333333','key-1','A','B',10,'PENDING')`, "duplicate key")
	checkRejected("duplicate debit ledger", `INSERT INTO ledger_entries(transfer_id,wallet_id,entry_type,amount)
        VALUES ('11111111-1111-4111-8111-111111111111','reserve','DEBIT',200)`, "duplicate key")
	checkRejected("immutable ledger update", `UPDATE ledger_entries SET amount = 20 WHERE entry_type = 'DEBIT'`, "immutable")
	checkRejected("immutable ledger delete", `DELETE FROM ledger_entries WHERE entry_type = 'DEBIT'`, "immutable")

	if got := run("SELECT count(*) FROM transfers"); got != "1" {
		t.Fatal(fmt.Sprintf("constraint tests changed transfer count: %s", got))
	}
}
