package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/testutil"
)

// These tests prove the database itself rejects invalid states, independent of
// any application code.

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

func seedWallets(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Now()
	mustExec(t, pool, `INSERT INTO wallets (id, balance, opening_balance, created_at, updated_at)
		VALUES ('a', 100, 100, $1, $1), ('b', 0, 0, $1, $1)`, now)
}

func insertTransfer(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, key string) {
	t.Helper()
	mustExec(t, pool, `INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		VALUES ($1, $2, 'a', 'b', 10, 'PENDING', now(), now())`, id, key)
}

func TestSchema_RejectsNegativeBalance(t *testing.T) {
	pool := testutil.NewPool(t)
	seedWallets(t, pool)

	_, err := pool.Exec(context.Background(), `UPDATE wallets SET balance = -1 WHERE id = 'a'`)

	assert.Error(t, err, "CHECK (balance >= 0) must stop an overdraft even if application code is wrong")
}

func TestSchema_RejectsDuplicateIdempotencyKeyOnTransfers(t *testing.T) {
	pool := testutil.NewPool(t)
	seedWallets(t, pool)
	insertTransfer(t, pool, uuid.New(), "k1")

	_, err := pool.Exec(context.Background(),
		`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		 VALUES ($1, 'k1', 'a', 'b', 10, 'PENDING', now(), now())`, uuid.New())

	assert.Error(t, err)
}

func TestSchema_RejectsInvalidTransfers(t *testing.T) {
	pool := testutil.NewPool(t)
	seedWallets(t, pool)
	ctx := context.Background()

	cases := map[string]string{
		"zero amount":       `INSERT INTO transfers VALUES (gen_random_uuid(), 'k', 'a', 'b', 0, 'PENDING', NULL, now(), now())`,
		"self transfer":     `INSERT INTO transfers VALUES (gen_random_uuid(), 'k', 'a', 'a', 5, 'PENDING', NULL, now(), now())`,
		"unknown status":    `INSERT INTO transfers VALUES (gen_random_uuid(), 'k', 'a', 'b', 5, 'DONE', NULL, now(), now())`,
		"failed no reason":  `INSERT INTO transfers VALUES (gen_random_uuid(), 'k', 'a', 'b', 5, 'FAILED', NULL, now(), now())`,
		"reason not failed": `INSERT INTO transfers VALUES (gen_random_uuid(), 'k', 'a', 'b', 5, 'PENDING', 'X', now(), now())`,
		"unknown wallet":    `INSERT INTO transfers VALUES (gen_random_uuid(), 'k', 'a', 'ghost', 5, 'PENDING', NULL, now(), now())`,
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(ctx, sql)
			assert.Error(t, err)
		})
	}
}

func TestSchema_LedgerRequiresExistingTransfer(t *testing.T) {
	pool := testutil.NewPool(t)
	seedWallets(t, pool)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
		 VALUES ($1, 'a', 'DEBIT', 10, now())`, uuid.New())

	assert.Error(t, err, "ledger rows cannot exist without a transfer")
}

func TestSchema_LedgerMustBalanceAtCommit(t *testing.T) {
	pool := testutil.NewPool(t)
	seedWallets(t, pool)
	ctx := context.Background()
	id := uuid.New()
	insertTransfer(t, pool, id, "k1")

	t.Run("single entry is rejected", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
			 VALUES ($1, 'a', 'DEBIT', 10, now())`, id)
		assert.Error(t, err)
	})

	t.Run("unequal amounts are rejected", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
			VALUES ($1, 'a', 'DEBIT', 10, now()), ($1, 'b', 'CREDIT', 9, now())`, id)
		require.NoError(t, err, "constraint trigger is deferred until commit")
		assert.Error(t, tx.Commit(ctx))
	})

	t.Run("a balanced pair commits", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
			VALUES ($1, 'a', 'DEBIT', 10, now()), ($1, 'b', 'CREDIT', 10, now())`, id)
		assert.NoError(t, err)
	})

	t.Run("a third entry is rejected", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
			VALUES ($1, 'a', 'DEBIT', 10, now())`, id)
		assert.Error(t, err, "UNIQUE (transfer_id, type) allows at most one DEBIT and one CREDIT")
	})
}

func TestSchema_LedgerIsAppendOnly(t *testing.T) {
	pool := testutil.NewPool(t)
	seedWallets(t, pool)
	ctx := context.Background()
	id := uuid.New()
	insertTransfer(t, pool, id, "k1")
	mustExec(t, pool, `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
		VALUES ($1, 'a', 'DEBIT', 10, now()), ($1, 'b', 'CREDIT', 10, now())`, id)

	_, err := pool.Exec(ctx, `UPDATE ledger_entries SET amount = 1`)
	assert.Error(t, err, "ledger rows must not be updated")

	_, err = pool.Exec(ctx, `DELETE FROM ledger_entries`)
	assert.Error(t, err, "ledger rows must not be deleted")
}
