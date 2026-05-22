package service_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/repository"
	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/service"
)

// testPool is the connection to the test database, or nil when DATABASE_URL is
// unset (in which case the integration tests are skipped).
var testPool *pgxpool.Pool

// TestMain connects to the database named by DATABASE_URL and applies the
// schema once for the whole package. If DATABASE_URL is unset, integration
// tests are skipped so `go test ./...` still passes without a database.
//
// Per-test isolation is achieved with unique (UUID) wallet IDs rather than
// truncating tables.
func TestMain(m *testing.M) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		os.Exit(m.Run()) // no database: requireDB skips the integration tests
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	if err := resetSchema(ctx, pool); err != nil {
		log.Fatalf("reset schema: %v", err)
	}
	if err := applyMigration(ctx, pool); err != nil {
		log.Fatalf("apply migration: %v", err)
	}
	testPool = pool

	code := m.Run()

	pool.Close()
	os.Exit(code)
}

// resetSchema drops and recreates the public schema so each test run starts
// from a clean database. Safe because tests run against a throwaway database.
func resetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
	return err
}

// applyMigration executes the schema file statement by statement (pgx's
// extended protocol runs one command per Exec). Statements are separated by
// semicolons, which appear only as terminators in this migration.
func applyMigration(ctx context.Context, pool *pgxpool.Pool) error {
	raw, err := os.ReadFile("../../migrations/0001_init.sql")
	if err != nil {
		return err
	}
	for stmt := range strings.SplitSeq(string(raw), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec statement: %w", err)
		}
	}
	return nil
}

// ---- helpers -------------------------------------------------------------

// requireDB skips the calling test when no database is configured.
func requireDB(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
}

func newService() *service.TransferService {
	return service.NewTransferService(repository.NewStore(testPool))
}

// seedWallet inserts a wallet with the given balance and returns its ID.
func seedWallet(t *testing.T, balance int64) string {
	t.Helper()
	id := uuid.NewString()
	_, err := testPool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance)
	require.NoError(t, err)
	return id
}

// walletBalance reads the stored balance of a wallet.
func walletBalance(t *testing.T, id string) int64 {
	t.Helper()
	var bal int64
	err := testPool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, id).Scan(&bal)
	require.NoError(t, err)
	return bal
}

// ledgerNet returns SUM(credits) - SUM(debits) recorded for a wallet.
func ledgerNet(t *testing.T, walletID string) int64 {
	t.Helper()
	var net int64
	err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END), 0)
		FROM ledger_entries WHERE wallet_id = $1`, walletID).Scan(&net)
	require.NoError(t, err)
	return net
}

// transferCountForKey returns how many transfer rows exist for an idempotency key.
func transferCountForKey(t *testing.T, key string) int {
	t.Helper()
	var n int
	err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM transfers t
		JOIN idempotency_records i ON i.transfer_id = t.id
		WHERE i.idempotency_key = $1`, key).Scan(&n)
	require.NoError(t, err)
	return n
}

// runConcurrently launches n goroutines that all start at the same instant
// (released by a shared barrier) and returns once every one has finished.
func runConcurrently(n int, work func(i int)) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			work(i)
		}(i)
	}
	close(start)
	wg.Wait()
}

// atomicCounter is a tiny helper for tallying outcomes across goroutines.
type atomicCounter struct{ v atomic.Int64 }

func (c *atomicCounter) inc()       { c.v.Add(1) }
func (c *atomicCounter) get() int64 { return c.v.Load() }
