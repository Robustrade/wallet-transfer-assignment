package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/service"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateTransfer_PostgresIdempotencyReplayReturnsOriginalResponse(t *testing.T) {
	pool := newPostgresTestPool(t)
	seedWallets(t, pool, map[string]int64{
		"wallet_1": 1000,
		"wallet_2": 500,
	})

	svc := service.NewTransferService(repository.NewPostgresStore(pool))

	first, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "postgres_replay_key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	second, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "postgres_replay_key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("second transfer failed: %v", err)
	}

	if first.Response.TransferID != second.Response.TransferID {
		t.Fatalf(
			"expected replayed transfer id %s, got %s",
			first.Response.TransferID,
			second.Response.TransferID,
		)
	}

	if !second.Replayed {
		t.Fatalf("expected second request to replay stored response")
	}

	if balance := fetchWalletBalance(t, pool, "wallet_1"); balance != 900 {
		t.Fatalf("expected wallet_1 balance 900, got %d", balance)
	}

	if balance := fetchWalletBalance(t, pool, "wallet_2"); balance != 600 {
		t.Fatalf("expected wallet_2 balance 600, got %d", balance)
	}

	if count := countRows(t, pool, "SELECT COUNT(*) FROM transfers"); count != 1 {
		t.Fatalf("expected exactly 1 transfer row, got %d", count)
	}

	if count := countRows(t, pool, "SELECT COUNT(*) FROM ledger_entries"); count != 2 {
		t.Fatalf("expected exactly 2 ledger rows, got %d", count)
	}

	if count := countRows(t, pool, "SELECT COUNT(*) FROM idempotency_records"); count != 1 {
		t.Fatalf("expected exactly 1 idempotency row, got %d", count)
	}
}

func TestCreateTransfer_PostgresConcurrentTransfersDoNotOverspend(t *testing.T) {
	pool := newPostgresTestPool(t)
	seedWallets(t, pool, map[string]int64{
		"wallet_1": 100,
		"wallet_2": 0,
		"wallet_3": 0,
	})

	svc := service.NewTransferService(repository.NewPostgresStore(pool))

	commands := []service.CreateTransferCommand{
		{
			IdempotencyKey: "postgres_concurrent_1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         80,
		},
		{
			IdempotencyKey: "postgres_concurrent_2",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_3",
			Amount:         80,
		},
	}

	start := make(chan struct{})
	results := make(chan *service.CreateTransferResult, len(commands))
	errs := make(chan error, len(commands))

	var wg sync.WaitGroup

	for _, cmd := range commands {
		wg.Add(1)

		go func(cmd service.CreateTransferCommand) {
			defer wg.Done()
			<-start

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, err := svc.CreateTransfer(ctx, cmd)
			if err != nil {
				errs <- err
				return
			}

			results <- result
		}(cmd)
	}

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected concurrent transfer error: %v", err)
		}
	}

	var processed int
	var failed int

	for result := range results {
		switch result.Response.Status {
		case string(domain.TransferStatusProcessed):
			processed++

		case string(domain.TransferStatusFailed):
			failed++

		default:
			t.Fatalf("unexpected transfer status %s", result.Response.Status)
		}
	}

	if processed != 1 {
		t.Fatalf("expected 1 processed transfer, got %d", processed)
	}

	if failed != 1 {
		t.Fatalf("expected 1 failed transfer, got %d", failed)
	}

	if balance := fetchWalletBalance(t, pool, "wallet_1"); balance != 20 {
		t.Fatalf("expected wallet_1 balance 20, got %d", balance)
	}

	totalCredits := fetchWalletBalance(t, pool, "wallet_2") + fetchWalletBalance(t, pool, "wallet_3")
	if totalCredits != 80 {
		t.Fatalf("expected total credited balance 80, got %d", totalCredits)
	}

	if count := countRows(t, pool, "SELECT COUNT(*) FROM ledger_entries"); count != 2 {
		t.Fatalf("expected 2 ledger rows for one successful transfer, got %d", count)
	}

	if count := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers WHERE status = 'PROCESSED'",
	); count != 1 {
		t.Fatalf("expected 1 processed transfer row, got %d", count)
	}

	if count := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers WHERE status = 'FAILED'",
	); count != 1 {
		t.Fatalf("expected 1 failed transfer row, got %d", count)
	}
}

func newPostgresTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run Postgres integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create admin pool: %v", err)
	}

	schemaName := "wallet_transfer_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")

	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		adminPool.Close()
		t.Fatalf("create schema %s: %v", schemaName, err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		adminPool.Close()
		t.Fatalf("parse database url: %v", err)
	}

	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = map[string]string{}
	}

	poolConfig.ConnConfig.RuntimeParams["search_path"] = schemaName

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		adminPool.Close()
		t.Fatalf("create test pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		adminPool.Close()
		t.Fatalf("ping test pool: %v", err)
	}

	if _, err := pool.Exec(ctx, readRepoFile(t, "migrations", "001_init.sql")); err != nil {
		pool.Close()
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		adminPool.Close()
		t.Fatalf("apply schema migration: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()

		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()

		if _, err := adminPool.Exec(dropCtx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName)); err != nil {
			t.Errorf("drop schema %s: %v", schemaName, err)
		}

		adminPool.Close()
	})

	return pool
}

func seedWallets(t *testing.T, pool *pgxpool.Pool, walletBalances map[string]int64) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const query = `
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2);
	`

	for walletID, balance := range walletBalances {
		if _, err := pool.Exec(ctx, query, walletID, balance); err != nil {
			t.Fatalf("seed wallet %s: %v", walletID, err)
		}
	}
}

func fetchWalletBalance(t *testing.T, pool *pgxpool.Pool, walletID string) int64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var balance int64

	if err := pool.QueryRow(
		ctx,
		"SELECT balance FROM wallets WHERE id = $1",
		walletID,
	).Scan(&balance); err != nil {
		t.Fatalf("fetch wallet balance for %s: %v", walletID, err)
	}

	return balance
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string) int64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int64

	if err := pool.QueryRow(ctx, query).Scan(&count); err != nil {
		t.Fatalf("count rows for %q: %v", query, err)
	}

	return count
}

func readRepoFile(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}

	repoRoot := filepath.Dir(filepath.Dir(currentFile))
	absolutePath := filepath.Join(append([]string{repoRoot}, pathParts...)...)

	content, err := os.ReadFile(absolutePath)
	if err != nil {
		t.Fatalf("read %s: %v", absolutePath, err)
	}

	return string(content)
}
