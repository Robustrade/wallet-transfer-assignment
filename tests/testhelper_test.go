package tests

import (
	"context"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/repository"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/service"
)

// newTestServices connects to a Postgres instance (DATABASE_URL env var, or
// a sane local default matching docker-compose.yml) and wires up the real
// service layer against the real schema. Tests are skipped if the database
// is unreachable, so `go test` remains usable without Postgres running.
func newTestServices(t *testing.T) (*service.TransferService, *service.WalletService, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: database not reachable at %s: %v", dsn, err)
	}

	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()

	transferService := service.NewTransferService(pool, walletRepo, transferRepo, ledgerRepo)
	walletService := service.NewWalletService(pool, walletRepo)

	t.Cleanup(pool.Close)

	return transferService, walletService, pool
}

// resetSchema truncates all tables so each test starts from a clean slate.
func resetSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE ledger_entries, transfers, wallets CASCADE`)
	if err != nil {
		t.Fatalf("resetting schema: %v", err)
	}
}

// idSeq guarantees uniqueID never collides even when called concurrently
// from many goroutines within the same timer tick (coarse clock
// resolution on some platforms can otherwise produce identical
// nanosecond-formatted timestamps).
var idSeq int64

// uniqueID returns a short unique suffix so parallel/successive test runs
// (including concurrent goroutines within a single test) don't collide on
// wallet ids or idempotency keys.
func uniqueID(prefix string) string {
	n := atomic.AddInt64(&idSeq, 1)
	return prefix + "-" + time.Now().Format("150405.000000000") + "-" + strconv.FormatInt(n, 10)
}

// mustCreateWallet seeds a wallet with a starting balance, failing the test
// immediately on error.
func mustCreateWallet(t *testing.T, walletSvc *service.WalletService, id string, balance int64) domain.Wallet {
	t.Helper()
	w, err := walletSvc.CreateWallet(context.Background(), id, balance)
	if err != nil {
		t.Fatalf("creating wallet %s: %v", id, err)
	}
	return w
}

// mustListLedgerEntries fetches ledger entries for a transfer directly via
// the pool, bypassing the service layer, so tests assert on persisted state
// rather than implementation details of how entries were written.
func mustListLedgerEntries(t *testing.T, pool *pgxpool.Pool, transferID string) []domain.LedgerEntry {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY created_at
	`, transferID)
	if err != nil {
		t.Fatalf("querying ledger entries: %v", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			t.Fatalf("scanning ledger entry: %v", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating ledger entries: %v", err)
	}
	return entries
}
