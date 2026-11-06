package migrate_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/platform/migrate"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil/pgtest"
	"github.com/Robustrade/wallet-transfer-assignment/migrations"
)

func TestUp_AppliesSchemaOnceAndIsANoOpOnRerun(t *testing.T) {
	pool := pgtest.NewEmptyDB(t)
	ctx := context.Background()

	applied, err := migrate.Up(ctx, pool, migrations.FS, pgtest.Logger())
	require.NoError(t, err)
	require.Equal(t, []string{"0001_init"}, applied)

	for _, table := range []string{"wallets", "transfers", "ledger_entries", "idempotency_records"} {
		require.Truef(t, tableExists(t, pool, table), "table %s should exist", table)
	}

	appliedAgain, err := migrate.Up(ctx, pool, migrations.FS, pgtest.Logger())
	require.NoError(t, err)
	require.Empty(t, appliedAgain, "re-running migrations must not apply anything")
}

func TestUp_ConcurrentRunnersApplyEachMigrationExactlyOnce(t *testing.T) {
	pool := pgtest.NewEmptyDB(t)
	ctx := context.Background()

	const runners = 5
	var wg sync.WaitGroup
	errs := make(chan error, runners)
	for range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := migrate.Up(ctx, pool, migrations.FS, pgtest.Logger())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	var recorded int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&recorded))
	require.Equal(t, 1, recorded)
}

func TestSeed_IsIdempotentAndNeverResetsBalances(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	require.NoError(t, migrate.Seed(ctx, pool, migrations.FS, pgtest.Logger()))
	// Simulate real activity changing a balance between restarts.
	_, err := pool.Exec(ctx, `UPDATE wallets SET balance = 42 WHERE id = 'wallet_1'`)
	require.NoError(t, err)

	require.NoError(t, migrate.Seed(ctx, pool, migrations.FS, pgtest.Logger()))

	var wallets int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM wallets`).Scan(&wallets))
	require.Equal(t, 3, wallets)

	var balance int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = 'wallet_1'`).Scan(&balance))
	require.Equal(t, int64(42), balance, "re-seeding must not overwrite balances")
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&exists))
	return exists
}
