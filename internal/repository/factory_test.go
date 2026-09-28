package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

func sqliteOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Driver:      "sqlite",
		DSN:         "file:" + filepath.Join(t.TempDir(), "f.db"),
		LockTimeout: time.Second,
	}
}

func TestOpen_UnsupportedDriver(t *testing.T) {
	t.Parallel()
	for _, driver := range []string{"", "mysql", "SQLITE"} {
		store, err := Open(context.Background(), Options{Driver: driver, DSN: "x", LockTimeout: time.Second})
		require.Error(t, err, driver)
		assert.Contains(t, err.Error(), "unsupported")
		assert.Nil(t, store)
	}
}

func TestOpen_MigratesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	opts := sqliteOptions(t)

	for i := 0; i < 2; i++ { // the second Open must see the schema and skip migrations
		store, err := Open(context.Background(), opts)
		require.NoError(t, err)
		var n int
		require.NoError(t, store.DB.QueryRow("SELECT COUNT(*) FROM wallets").Scan(&n))
		assert.Equal(t, 0, n, "no demo data unless requested")
		require.NoError(t, store.Close())
	}
}

func TestOpen_SeedsDemoDataWhenRequested(t *testing.T) {
	t.Parallel()
	opts := sqliteOptions(t)
	opts.SeedDemoData = true

	store, err := Open(context.Background(), opts)
	require.NoError(t, err)
	defer store.Close()

	want := map[string]struct {
		balance  int64
		currency string
	}{
		"wallet_1":   {100000, "SGD"},
		"wallet_2":   {50000, "SGD"},
		"wallet_3":   {0, "SGD"},
		"wallet_usd": {10000, "USD"},
	}
	for id, w := range want {
		got, err := store.Repos.Wallets.Get(context.Background(), id)
		require.NoError(t, err, id)
		assert.Equal(t, w.balance, got.Balance, id)
		assert.Equal(t, w.currency, got.Currency, id)
	}
	_, err = store.Repos.Wallets.Get(context.Background(), "wallet_4")
	assert.ErrorIs(t, err, domain.ErrWalletNotFound)
}

func TestOpen_UnreachableDatabaseFails(t *testing.T) {
	t.Parallel()
	_, err := Open(context.Background(), Options{
		Driver:      "sqlite",
		DSN:         "file:" + filepath.Join(t.TempDir(), "no-such-dir", "x.db"),
		LockTimeout: time.Second,
	})
	require.Error(t, err)
}

// TestOpen_SQLiteWritesUseASingleConnection deliberately checks configuration
// rather than behavior. The behavior (no SQLITE_BUSY starvation under a
// write burst) is covered by TestConcurrency_StressRandomTransfers, but it
// fails only intermittently when the pool regresses. This test fails
// deterministically, and points at the cause.
func TestOpen_SQLiteWritesUseASingleConnection(t *testing.T) {
	t.Parallel()
	store, err := Open(context.Background(), sqliteOptions(t))
	require.NoError(t, err)
	defer store.Close()

	// One writer connection: write transactions queue in database/sql
	// instead of busy-polling each other inside SQLite.
	assert.Equal(t, 1, store.writeDB.Stats().MaxOpenConnections)
	assert.Greater(t, store.DB.Stats().MaxOpenConnections, 1, "reads keep a pool (WAL readers are concurrent)")
	assert.NotSame(t, store.DB, store.writeDB)
}
