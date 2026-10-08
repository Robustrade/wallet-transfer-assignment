//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
)

// Each test gets a migrated schema inside the package-shared container.
func postgresTestDB(t *testing.T) (*sql.DB, context.Context, string) {
	t.Helper()
	dsn := containerDSN(t)
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid container connection string")
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	schema := fmt.Sprintf("transfer_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.ExecContext(cleanupCtx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	cfg.RuntimeParams["search_path"] = schema
	cfg.RuntimeParams["application_name"] = schema
	db := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { db.Close() })
	migration, err := os.ReadFile("../../migrations/000001_initial.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}

	return db, ctx, schema
}

func TestPostgresTransferAtomicity(t *testing.T) {
	for _, scenario := range []string{"success", "insufficient", "missing", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			db, ctx, _ := postgresTestDB(t)
			if _, err := db.ExecContext(ctx, `INSERT INTO wallets(id,balance) VALUES ('a',100),('b',20)`); err != nil {
				t.Fatal(err)
			}
			if scenario == "missing" {
				if _, err := db.ExecContext(ctx, `DELETE FROM wallets WHERE id='b'`); err != nil {
					t.Fatal(err)
				}
			}
			var work repository.UnitOfWork = postgres.New(db)
			if scenario == "rollback" {
				work = failCreditWork{work}
			}
			service := New(work, slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := request()
			if scenario == "insufficient" {
				req.Amount = 101
			}
			result, err := service.Transfer(ctx, req)
			if scenario == "rollback" || scenario == "missing" {
				if err == nil || result != (repository.Result{}) {
					t.Fatalf("expected error and no result: %+v %v", result, err)
				}
				assertCount(t, ctx, db, "transfers", 0)
				assertCount(t, ctx, db, "ledger_entries", 0)
				assertCount(t, ctx, db, "idempotency_records", 0)
				assertBalance(t, ctx, db, "a", 100)
				if scenario == "rollback" {
					assertBalance(t, ctx, db, "b", 20)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status := "PROCESSED"
			count := 2
			if scenario == "insufficient" {
				status = "FAILED"
				count = 0
				assertBalance(t, ctx, db, "a", 100)
				assertBalance(t, ctx, db, "b", 20)
			} else {
				assertBalance(t, ctx, db, "a", 70)
				assertBalance(t, ctx, db, "b", 50)
			}
			var stored string
			if err := db.QueryRowContext(ctx, `SELECT status FROM transfers`).Scan(&stored); err != nil || stored != status {
				t.Fatalf("status %q: %v", stored, err)
			}
			assertCount(t, ctx, db, "transfers", 1)
			assertCount(t, ctx, db, "ledger_entries", count)
			if count == 2 {
				assertStoredBalancedLedger(t, ctx, db, req.IdempotencyKey)
			}
			if scenario == "insufficient" {
				if _, err := db.ExecContext(ctx, `UPDATE wallets SET balance=1000 WHERE id='a'`); err != nil {
					t.Fatal(err)
				}
			}
			replay, err := service.Transfer(ctx, req)
			if err != nil || replay != result {
				t.Fatalf("replay mismatch: %v", err)
			}
			assertCount(t, ctx, db, "transfers", 1)
			assertCount(t, ctx, db, "ledger_entries", count)
			if count == 2 {
				assertStoredBalancedLedger(t, ctx, db, req.IdempotencyKey)
			}
		})
	}
}

// Duplicate the debit primary key on the second insert to cause a real PostgreSQL
// 23505 after balances and the first entry changed. The adapter must roll it all back.
type failCreditWork struct{ repository.UnitOfWork }

func (w failCreditWork) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	return w.UnitOfWork.WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error { return fn(ctx, failCreditTx{tx}) })
}

type failCreditTx struct{ repository.Tx }

func (tx failCreditTx) CreateLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error {
	if entry.Type == domain.LedgerEntryCredit {
		entry.ID = strings.TrimSuffix(entry.ID, "-credit") + "-debit"
	}
	return tx.Tx.CreateLedgerEntry(ctx, entry)
}
func assertCount(t *testing.T, ctx context.Context, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&got); err != nil || got != want {
		t.Fatalf("%s count %d want %d: %v", table, got, want, err)
	}
}
func assertBalance(t *testing.T, ctx context.Context, db *sql.DB, id string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id=$1`, id).Scan(&got); err != nil || got != want {
		t.Fatalf("%s balance %d want %d: %v", id, got, want, err)
	}
}
