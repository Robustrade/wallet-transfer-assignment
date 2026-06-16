//go:build integration

package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	appmigrate "github.com/synchone/wallet-transfer-assignment/internal/migrate"
	"github.com/synchone/wallet-transfer-assignment/internal/repository"
	"github.com/synchone/wallet-transfer-assignment/internal/service"
)

type testEnv struct {
	pool    *pgxpool.Pool
	repo    *repository.Repository
	service *service.TransferService
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("wallet_transfer"),
		postgres.WithUsername("wallet"),
		postgres.WithPassword("wallet"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, container.Terminate(ctx))
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	require.NoError(t, appmigrate.Up(connStr))

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo := repository.New(pool)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := service.NewTransferService(repo, logger)

	return &testEnv{pool: pool, repo: repo, service: svc}
}

func resetWallets(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		TRUNCATE ledger_entries, transfers RESTART IDENTITY CASCADE;
		DELETE FROM wallets;
		INSERT INTO wallets (id, balance) VALUES
			('wallet_1', 10000),
			('wallet_2', 5000);
	`)
	require.NoError(t, err)
}

func walletBalance(t *testing.T, pool *pgxpool.Pool, walletID string) int64 {
	t.Helper()
	var balance int64
	err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&balance)
	require.NoError(t, err)
	return balance
}

func ledgerCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ledger_entries`).Scan(&count)
	require.NoError(t, err)
	return count
}

func transferCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM transfers`).Scan(&count)
	require.NoError(t, err)
	return count
}
