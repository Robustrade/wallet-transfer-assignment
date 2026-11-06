package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil/pgtest"
)

// These tests prove the database itself rejects invalid states, independent of
// application code (see docs/DESIGN.md §3).

const (
	checkViolation      = "23514"
	foreignKeyViolation = "23503"
	uniqueViolation     = "23505"
)

func TestSchema_RejectsInvalidStates(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	mustExec(t, pool, `INSERT INTO wallets (id, balance) VALUES ('a', 100), ('b', 0)`)
	transferID := uuid.New()
	mustExec(t, pool, `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
	                   VALUES ($1, 'a', 'b', 10, 'PROCESSED')`, transferID)
	mustExec(t, pool, `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
	                   VALUES ($1, 'a', 'DEBIT', 10)`, transferID)

	tests := []struct {
		name     string
		sql      string
		args     []any
		wantCode string
	}{
		{"negative wallet balance", `UPDATE wallets SET balance = -1 WHERE id = 'a'`, nil, checkViolation},
		{"self transfer", `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		                   VALUES ($1, 'a', 'a', 10, 'PROCESSED')`, []any{uuid.New()}, checkViolation},
		{"non-positive transfer amount", `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		                   VALUES ($1, 'a', 'b', 0, 'PROCESSED')`, []any{uuid.New()}, checkViolation},
		{"unknown transfer status", `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		                   VALUES ($1, 'a', 'b', 10, 'DONE')`, []any{uuid.New()}, checkViolation},
		{"failed transfer without reason", `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		                   VALUES ($1, 'a', 'b', 10, 'FAILED')`, []any{uuid.New()}, checkViolation},
		{"processed transfer with failure reason", `INSERT INTO transfers
		                   (id, from_wallet_id, to_wallet_id, amount, status, failure_reason)
		                   VALUES ($1, 'a', 'b', 10, 'PROCESSED', 'X')`, []any{uuid.New()}, checkViolation},
		{"transfer to unknown wallet", `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		                   VALUES ($1, 'a', 'zzz', 10, 'PROCESSED')`, []any{uuid.New()}, foreignKeyViolation},
		{"ledger entry without transfer", `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		                   VALUES ($1, 'a', 'DEBIT', 10)`, []any{uuid.New()}, foreignKeyViolation},
		{"second debit for the same transfer", `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		                   VALUES ($1, 'a', 'DEBIT', 10)`, []any{transferID}, uniqueViolation},
		{"non-positive ledger amount", `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		                   VALUES ($1, 'b', 'CREDIT', 0)`, []any{transferID}, checkViolation},
		{"idempotency record without transfer", `INSERT INTO idempotency_records (key, request_hash, transfer_id)
		                   VALUES ('k', 'h', $1)`, []any{uuid.New()}, foreignKeyViolation},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Run in an explicit transaction and COMMIT, so deferred constraints are checked too.
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()

			_, err = tx.Exec(ctx, tc.sql, tc.args...)
			if err == nil {
				err = tx.Commit(ctx)
			}
			requirePgError(t, err, tc.wantCode)
		})
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

func requirePgError(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.Truef(t, errors.As(err, &pgErr), "expected Postgres error %s, got %v", code, err)
	require.Equal(t, code, pgErr.Code, pgErr.Message)
}
