package migrate_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aks-95/wallet-transfer-assignment/internal/migrate"
	"github.com/aks-95/wallet-transfer-assignment/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:root@localhost:5432/wallet_transfer?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	db, err := repository.New(ctx, url)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	db.Close()

	return url
}

func TestMigrationNames(t *testing.T) {
	names, err := migrate.MigrationNames()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"001_create_wallets.up.sql",
		"002_create_transfers.up.sql",
		"003_create_ledger_entries.up.sql",
	}, names)
}

func TestUpCreatesSchema(t *testing.T) {
	databaseURL := testDatabaseURL(t)

	ctx := context.Background()
	db, err := repository.New(ctx, databaseURL)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, migrate.Up(ctx, db.Pool))
	require.NoError(t, migrate.Up(ctx, db.Pool), "second run should be idempotent")

	tables := []string{"wallets", "transfers", "ledger_entries", "schema_migrations"}
	for _, table := range tables {
		var exists bool
		err := db.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)
		`, table).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "expected table %s", table)
	}
}

func TestTransferConstraints(t *testing.T) {
	databaseURL := testDatabaseURL(t)

	ctx := context.Background()
	db, err := repository.New(ctx, databaseURL)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, migrate.Up(ctx, db.Pool))
	_, err = db.Pool.Exec(ctx, `TRUNCATE TABLE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	var fromWalletID, toWalletID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO wallets (balance) VALUES (1000) RETURNING id`).Scan(&fromWalletID)
	require.NoError(t, err)
	err = db.Pool.QueryRow(ctx, `INSERT INTO wallets (balance) VALUES (0) RETURNING id`).Scan(&toWalletID)
	require.NoError(t, err)

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ('dup-key', $1, $2, 100, 'PENDING')
	`, fromWalletID, toWalletID)
	require.NoError(t, err)

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ('dup-key', $1, $2, 50, 'PENDING')
	`, fromWalletID, toWalletID)
	require.Error(t, err, "duplicate idempotency key should fail")

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ('same-wallet', $1, $1, 50, 'PENDING')
	`, fromWalletID)
	require.Error(t, err, "same wallet transfer should fail")
}
