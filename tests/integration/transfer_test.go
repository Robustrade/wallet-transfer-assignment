package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/domain"
	appPostgres "github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/service"
)

func TestPostgresTransferIdempotencyAndLedger(t *testing.T) {
	pool := integrationPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool, map[string]int64{"wallet-1": 200, "wallet-2": 50})
	transferService := service.NewTransferService(appPostgres.NewUnitOfWork(pool))
	command := service.TransferCommand{
		IdempotencyKey: "integration-key-1",
		FromWalletID:   "wallet-1",
		ToWalletID:     "wallet-2",
		Amount:         100,
	}

	first, err := transferService.CreateTransfer(context.Background(), command)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	duplicate, err := transferService.CreateTransfer(context.Background(), command)
	if err != nil {
		t.Fatalf("duplicate transfer: %v", err)
	}

	if first.Transfer.ID != duplicate.Transfer.ID || !duplicate.Replayed {
		t.Fatalf("duplicate did not return original result: first=%#v duplicate=%#v", first, duplicate)
	}
	assertBalance(t, pool, "wallet-1", 100)
	assertBalance(t, pool, "wallet-2", 150)
	assertCount(t, pool, "SELECT count(*) FROM transfers", 1)
	assertCount(t, pool, "SELECT count(*) FROM ledger_entries", 2)
	assertLedgerEntry(t, pool, first.Transfer.ID, "wallet-1", "DEBIT", 100)
	assertLedgerEntry(t, pool, first.Transfer.ID, "wallet-2", "CREDIT", 100)

	_, err = transferService.CreateTransfer(context.Background(), service.TransferCommand{
		IdempotencyKey: command.IdempotencyKey,
		FromWalletID:   command.FromWalletID,
		ToWalletID:     command.ToWalletID,
		Amount:         101,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("payload mismatch error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestPostgresFailedTransferIsIdempotentlyReplayed(t *testing.T) {
	pool := integrationPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool, map[string]int64{"wallet-1": 20, "wallet-2": 50})
	transferService := service.NewTransferService(appPostgres.NewUnitOfWork(pool))
	command := service.TransferCommand{
		IdempotencyKey: "failed-key-1",
		FromWalletID:   "wallet-1",
		ToWalletID:     "wallet-2",
		Amount:         100,
	}

	first, err := transferService.CreateTransfer(context.Background(), command)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	if first.Transfer.Status != domain.TransferStatusFailed {
		t.Fatalf("first status = %q, want FAILED", first.Transfer.Status)
	}

	if _, err := pool.Exec(context.Background(), "UPDATE wallets SET balance = 200 WHERE id = 'wallet-1'"); err != nil {
		t.Fatalf("top up source wallet: %v", err)
	}
	duplicate, err := transferService.CreateTransfer(context.Background(), command)
	if err != nil {
		t.Fatalf("duplicate transfer: %v", err)
	}

	if first.Transfer.ID != duplicate.Transfer.ID || !duplicate.Replayed {
		t.Fatalf("duplicate did not return original failure: first=%#v duplicate=%#v", first, duplicate)
	}
	if duplicate.Transfer.Status != domain.TransferStatusFailed {
		t.Fatalf("duplicate status = %q, want FAILED", duplicate.Transfer.Status)
	}
	assertBalance(t, pool, "wallet-1", 200)
	assertBalance(t, pool, "wallet-2", 50)
	assertCount(t, pool, "SELECT count(*) FROM transfers", 1)
	assertCount(t, pool, "SELECT count(*) FROM ledger_entries", 0)
}

func TestPostgresConcurrentDebitsDoNotOverspend(t *testing.T) {
	pool := integrationPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool, map[string]int64{
		"source":        100,
		"destination-1": 0,
		"destination-2": 0,
	})
	transferService := service.NewTransferService(appPostgres.NewUnitOfWork(pool))
	commands := []service.TransferCommand{
		{
			IdempotencyKey: "concurrent-key-1",
			FromWalletID:   "source",
			ToWalletID:     "destination-1",
			Amount:         80,
		},
		{
			IdempotencyKey: "concurrent-key-2",
			FromWalletID:   "source",
			ToWalletID:     "destination-2",
			Amount:         80,
		},
	}

	var waitGroup sync.WaitGroup
	results := make(chan *service.TransferResult, len(commands))
	errs := make(chan error, len(commands))
	for _, command := range commands {
		waitGroup.Add(1)
		go func(command service.TransferCommand) {
			defer waitGroup.Done()
			result, err := transferService.CreateTransfer(context.Background(), command)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(command)
	}
	waitGroup.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Errorf("concurrent transfer: %v", err)
	}
	processed, failed := 0, 0
	for result := range results {
		switch result.Transfer.Status {
		case domain.TransferStatusProcessed:
			processed++
		case domain.TransferStatusFailed:
			failed++
		}
	}
	if processed != 1 || failed != 1 {
		t.Fatalf("processed=%d failed=%d, want 1 and 1", processed, failed)
	}
	assertBalance(t, pool, "source", 20)
	assertCount(t, pool, "SELECT count(*) FROM ledger_entries", 2)
	assertCount(t, pool, "SELECT count(*) FROM transfers", 2)
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("INTEGRATION_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	applyMigration(t, pool)
	return pool
}

func applyMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(
		context.Background(),
		"SELECT to_regclass('public.wallets') IS NOT NULL",
	).Scan(&exists); err != nil {
		t.Fatalf("check schema: %v", err)
	}
	if exists {
		return
	}
	path := filepath.Join("..", "..", "migrations", "001_init.sql")
	migration, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(migration)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}

func resetDatabase(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE TABLE ledger_entries, idempotency_records, transfers, wallets
	`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

func seedWallets(t *testing.T, pool *pgxpool.Pool, wallets map[string]int64) {
	t.Helper()
	for id, balance := range wallets {
		if _, err := pool.Exec(
			context.Background(),
			"INSERT INTO wallets (id, balance) VALUES ($1, $2)",
			id,
			balance,
		); err != nil {
			t.Fatalf("seed wallet %q: %v", id, err)
		}
	}
}

func assertBalance(t *testing.T, pool *pgxpool.Pool, walletID string, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(
		context.Background(),
		"SELECT balance FROM wallets WHERE id = $1",
		walletID,
	).Scan(&got); err != nil {
		t.Fatalf("read balance for %q: %v", walletID, err)
	}
	if got != want {
		t.Fatalf("balance for %q = %d, want %d", walletID, got, want)
	}
}

func assertCount(t *testing.T, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func assertLedgerEntry(
	t *testing.T,
	pool *pgxpool.Pool,
	transferID string,
	walletID string,
	entryType string,
	amount int64,
) {
	t.Helper()
	var got int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*)
		FROM ledger_entries
		WHERE transfer_id = $1
			AND wallet_id = $2
			AND entry_type = $3
			AND amount = $4`,
		transferID,
		walletID,
		entryType,
		amount,
	).Scan(&got); err != nil {
		t.Fatalf("query ledger entry: %v", err)
	}
	if got != 1 {
		t.Fatalf("ledger entries for transfer=%q wallet=%q type=%q amount=%d = %d, want 1",
			transferID,
			walletID,
			entryType,
			amount,
			got,
		)
	}
}
