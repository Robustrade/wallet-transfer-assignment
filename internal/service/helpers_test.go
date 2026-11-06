package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil/pgtest"
)

// fixture is a transfer service on its own database, plus the opening balances
// needed to check ledger invariants afterwards.
type fixture struct {
	svc     *service.TransferService
	pool    *pgxpool.Pool
	opening map[string]int64
}

func newFixture(t *testing.T, opening map[string]int64) *fixture {
	t.Helper()
	pool := pgtest.NewDB(t)
	for id, balance := range opening {
		_, err := pool.Exec(context.Background(), `INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance)
		require.NoError(t, err)
	}
	db := postgres.New(pool, postgres.Options{LockTimeout: 10 * time.Second})
	return &fixture{
		svc:     service.NewTransferService(db),
		pool:    pool,
		opening: opening,
	}
}

func request(key, from, to string, amount int64) domain.TransferRequest {
	return domain.TransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

func newKey() string { return "key-" + uuid.NewString() }

func (f *fixture) balance(t *testing.T, walletID string) int64 {
	t.Helper()
	var b int64
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&b))
	return b
}

func (f *fixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	require.NoError(t, f.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n))
	return n
}

type ledgerRow struct {
	WalletID string
	Type     string
	Amount   int64
}

func (f *fixture) ledgerFor(t *testing.T, transferID uuid.UUID) []ledgerRow {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT wallet_id, type, amount FROM ledger_entries WHERE transfer_id = $1 ORDER BY type DESC`, transferID)
	require.NoError(t, err)
	defer rows.Close()
	var out []ledgerRow
	for rows.Next() {
		var r ledgerRow
		require.NoError(t, rows.Scan(&r.WalletID, &r.Type, &r.Amount))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// requireLedgerConsistent checks the global double-entry invariants:
//   - total debits equal total credits
//   - every PROCESSED transfer has exactly one DEBIT (source) and one CREDIT (destination) of its amount
//   - every non-processed transfer has no entries
//   - every wallet balance equals its opening balance + credits - debits
//   - no money was created or destroyed
func (f *fixture) requireLedgerConsistent(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	var debits, credits int64
	require.NoError(t, f.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'), 0),
		       COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0)
		FROM ledger_entries`).Scan(&debits, &credits))
	require.Equal(t, debits, credits, "ledger must balance")

	var badTransfers int
	require.NoError(t, f.pool.QueryRow(ctx, `
		SELECT count(*) FROM transfers t
		WHERE (t.status = 'PROCESSED' AND (
		         (SELECT count(*) FROM ledger_entries e WHERE e.transfer_id = t.id) <> 2
		      OR NOT EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id
		                     AND e.type = 'DEBIT' AND e.wallet_id = t.from_wallet_id AND e.amount = t.amount)
		      OR NOT EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id
		                     AND e.type = 'CREDIT' AND e.wallet_id = t.to_wallet_id AND e.amount = t.amount)))
		   OR (t.status <> 'PROCESSED' AND EXISTS (SELECT 1 FROM ledger_entries e WHERE e.transfer_id = t.id))
	`).Scan(&badTransfers))
	require.Zero(t, badTransfers, "every processed transfer needs a matching debit/credit pair; others none")

	var totalOpening, totalNow int64
	for id, opening := range f.opening {
		var net int64
		require.NoError(t, f.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(CASE type WHEN 'CREDIT' THEN amount ELSE -amount END), 0)
			FROM ledger_entries WHERE wallet_id = $1`, id).Scan(&net))
		balance := f.balance(t, id)
		require.Equalf(t, opening+net, balance, "wallet %s balance must equal opening + credits - debits", id)
		totalOpening += opening
		totalNow += balance
	}
	require.Equal(t, totalOpening, totalNow, "transfers must neither create nor destroy money")
}
