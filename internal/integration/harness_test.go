package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/service"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/testutil"
)

// env is a service stack wired to a fresh, migrated PostgreSQL database.
type env struct {
	pool      *pgxpool.Pool
	store     repository.Store
	transfers *service.TransferService
	wallets   *service.WalletService
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testutil.NewPool(t)
	return newEnvWithStore(pool, postgres.NewStore(pool))
}

func newEnvWithStore(pool *pgxpool.Pool, store repository.Store) *env {
	return &env{
		pool:      pool,
		store:     store,
		transfers: service.NewTransferService(store),
		wallets:   service.NewWalletService(store),
	}
}

func (e *env) wallet(t *testing.T, id string, balance int64) {
	t.Helper()
	_, err := e.wallets.CreateWallet(context.Background(), id, balance)
	require.NoError(t, err)
}

func (e *env) balance(t *testing.T, id string) int64 {
	t.Helper()
	w, err := e.wallets.GetWallet(context.Background(), id)
	require.NoError(t, err)
	return w.Balance
}

func (e *env) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

// assertLedgerReconciles checks the system-wide invariants that must hold after
// any sequence of transfers, concurrent or not.
func (e *env) assertLedgerReconciles(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	var totalDebit, totalCredit int64
	require.NoError(t, e.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(amount) FILTER (WHERE type = 'DEBIT'), 0),
		       COALESCE(sum(amount) FILTER (WHERE type = 'CREDIT'), 0)
		  FROM ledger_entries`).Scan(&totalDebit, &totalCredit))
	assert.Equal(t, totalDebit, totalCredit, "ledger must balance: total debits == total credits")

	rows, err := e.pool.Query(ctx, `
		SELECT w.id
		  FROM wallets w
		  LEFT JOIN (SELECT wallet_id,
		                    sum(CASE type WHEN 'CREDIT' THEN amount ELSE -amount END) AS net
		               FROM ledger_entries GROUP BY wallet_id) l ON l.wallet_id = w.id
		 WHERE w.balance <> w.opening_balance + COALESCE(l.net, 0)`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		t.Errorf("wallet %s: stored balance does not equal opening balance + ledger net", id)
	}
	require.NoError(t, rows.Err())

	bad, err := e.pool.Query(ctx, `
		SELECT t.id::text, t.status, count(e.id)
		  FROM transfers t LEFT JOIN ledger_entries e ON e.transfer_id = t.id
		 GROUP BY t.id, t.status
		HAVING (t.status = 'PROCESSED' AND count(e.id) <> 2)
		    OR (t.status <> 'PROCESSED' AND count(e.id) <> 0)
		    OR t.status = 'PENDING'`)
	require.NoError(t, err)
	defer bad.Close()
	for bad.Next() {
		var id, status string
		var n int
		require.NoError(t, bad.Scan(&id, &status, &n))
		t.Errorf("transfer %s is %s with %d ledger entries", id, status, n)
	}
	require.NoError(t, bad.Err())
}
