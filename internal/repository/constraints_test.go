package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/dbtest"
)

func TestSchemaRejectsInvalidState(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		now := time.Now().UTC()

		exec := func(query string, args ...any) error {
			_, err := db.ExecContext(ctx, db.Rebind(query), args...)
			return err
		}

		mustExec := func(query string, args ...any) {
			t.Helper()
			if err := exec(query, args...); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
		}

		mustExec(`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`, "alice", 100, now, now)
		mustExec(`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`, "bob", 0, now, now)
		mustExec(`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		          VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`, "t1", "alice", "bob", 100, "PROCESSED", now, now)
		mustExec(`INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
		          VALUES (?, ?, ?, ?, ?, ?)`, "e1", "t1", "alice", "DEBIT", 100, now)

		cases := []struct {
			name    string
			query   string
			args    []any
			violate func(error) bool
		}{
			{
				name:    "a wallet cannot go overdrawn",
				query:   `UPDATE wallets SET balance = balance - ? WHERE id = ?`,
				args:    []any{int64(200), "alice"},
				violate: db.IsCheckViolation,
			},
			{
				name:    "a transfer cannot have a non positive amount",
				query:   `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
				args:    []any{"t-zero", "alice", "bob", int64(0), "PENDING", now, now},
				violate: db.IsCheckViolation,
			},
			{
				name:    "a transfer cannot move money to itself",
				query:   `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
				args:    []any{"t-self", "alice", "alice", int64(10), "PENDING", now, now},
				violate: db.IsCheckViolation,
			},
			{
				name:    "a transfer cannot reference a wallet that does not exist",
				query:   `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
				args:    []any{"t-ghost", "alice", "ghost", int64(10), "PENDING", now, now},
				violate: db.IsForeignKeyViolation,
			},
			{
				name:    "a transfer cannot hold an unknown status",
				query:   `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
				args:    []any{"t-bad", "alice", "bob", int64(10), "SETTLED", now, now},
				violate: db.IsCheckViolation,
			},
			{
				name:    "a failed transfer must record why",
				query:   `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
				args:    []any{"t-silent", "alice", "bob", int64(10), "FAILED", now, now},
				violate: db.IsCheckViolation,
			},
			{
				name:    "a transfer cannot be debited twice",
				query:   `INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				args:    []any{"e-dup", "t1", "alice", "DEBIT", int64(100), now},
				violate: db.IsUniqueViolation,
			},
			{
				name:    "a ledger entry cannot exist without its transfer",
				query:   `INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				args:    []any{"e-orphan", "t-missing", "alice", "CREDIT", int64(10), now},
				violate: db.IsForeignKeyViolation,
			},
			{
				name:    "a ledger entry cannot take a side other than debit or credit",
				query:   `INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				args:    []any{"e-bad", "t1", "alice", "REVERSAL", int64(10), now},
				violate: db.IsCheckViolation,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := exec(tc.query, tc.args...)
				if err == nil {
					t.Fatal("database accepted a row it should have rejected")
				}
				if !tc.violate(err) {
					t.Fatalf("rejected for the wrong reason: %v", err)
				}
			})
		}
	})
}

func TestIdempotencyKeyIsUnique(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		now := time.Now().UTC()

		exec := func(query string, args ...any) error {
			_, err := db.ExecContext(ctx, db.Rebind(query), args...)
			return err
		}

		for _, stmt := range []struct {
			query string
			args  []any
		}{
			{`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`, []any{"alice", 100, now, now}},
			{`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`, []any{"bob", 0, now, now}},
			{`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`, []any{"t1", "alice", "bob", 10, "PROCESSED", now, now}},
			{`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`, []any{"t2", "alice", "bob", 10, "PROCESSED", now, now}},
			{`INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at) VALUES (?, ?, ?, ?)`, []any{"key-1", "hash", "t1", now}},
		} {
			if err := exec(stmt.query, stmt.args...); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
		}

		err := exec(`INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at) VALUES (?, ?, ?, ?)`,
			"key-1", "hash", "t2", now)
		if err == nil {
			t.Fatal("database accepted a second record for the same idempotency key")
		}
		if !db.IsUniqueViolation(err) {
			t.Fatalf("rejected for the wrong reason: %v", err)
		}
	})
}
