package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/config"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository/sqlrepo"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// testDrivers lists the databases to run against.
func testDrivers() []string {
	drivers := []string{config.DriverSQLite}
	if pgDSN != "" {
		drivers = append(drivers, config.DriverPostgres)
	}
	return drivers
}

// forEachDriver runs fn as a parallel subtest per database driver.
func forEachDriver(t *testing.T, fn func(t *testing.T, driver string)) {
	t.Helper()
	for _, driver := range testDrivers() {
		t.Run(driver, func(t *testing.T) {
			t.Parallel()
			fn(t, driver)
		})
	}
}

type envConfig struct {
	lockTimeout time.Duration
}

type envOption func(*envConfig)

// withLockTimeout sets LOCK_TIMEOUT for the environment's store.
func withLockTimeout(d time.Duration) envOption {
	return func(c *envConfig) { c.lockTimeout = d }
}

// testEnv is one fresh, migrated, empty database plus the bookkeeping the
// invariant checker needs.
type testEnv struct {
	t       *testing.T
	driver  string
	dsn     string
	store   *repository.Store
	db      *sql.DB
	dialect sqlrepo.Dialect

	mu      sync.Mutex
	opening map[string]int64 // wallet id -> opening balance, for invariants
}

// newEnv creates an isolated database for one test:
//   - SQLite: a new file in t.TempDir() (never :memory:, which would give
//     each pooled connection its own separate database);
//   - Postgres: a new randomly named schema, dropped at cleanup.
//
// After the test, the invariant checker runs, and then the store is closed.
func newEnv(t *testing.T, driver string, opts ...envOption) *testEnv {
	t.Helper()
	cfg := envConfig{lockTimeout: 5 * time.Second}
	for _, o := range opts {
		o(&cfg)
	}

	var dsn string
	switch driver {
	case config.DriverSQLite:
		dsn = "file:" + filepath.Join(t.TempDir(), "test.db")
	case config.DriverPostgres:
		dsn = newPostgresSchema(t)
	default:
		t.Fatalf("unknown driver %q", driver)
	}

	store, err := repository.Open(context.Background(), repository.Options{
		Driver:      driver,
		DSN:         dsn,
		LockTimeout: cfg.lockTimeout,
	})
	require.NoError(t, err)

	env := &testEnv{
		t:       t,
		driver:  driver,
		dsn:     dsn,
		store:   store,
		db:      store.DB,
		dialect: store.Dialect,
		opening: map[string]int64{},
	}
	// Cleanups run last-in-first-out: invariants first, then Close.
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(env.checkInvariants)
	return env
}

// newPostgresSchema creates a throwaway schema and returns a DSN whose
// search_path points at it.
func newPostgresSchema(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	_, err := rand.Read(b)
	require.NoError(t, err)
	schema := "test_" + hex.EncodeToString(b)

	_, err = pgAdmin.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pgAdmin.Exec("DROP SCHEMA " + schema + " CASCADE") })

	u, err := url.Parse(pgDSN)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// exec runs raw SQL written with "?" placeholders, bypassing the repositories.
// It is used to seed data and to prove the DB enforces constraints itself.
func (e *testEnv) exec(query string, args ...any) error {
	_, err := e.db.Exec(e.dialect.Rebind(query), args...)
	return err
}

// queryInt runs a query that returns a single integer.
func (e *testEnv) queryInt(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	require.NoError(e.t, e.db.QueryRow(e.dialect.Rebind(query), args...).Scan(&n))
	return n
}

func (e *testEnv) count(table string) int64 {
	e.t.Helper()
	return e.queryInt("SELECT COUNT(*) FROM " + table)
}

func (e *testEnv) balance(walletID string) int64 {
	e.t.Helper()
	return e.queryInt("SELECT balance FROM wallets WHERE id = ?", walletID)
}

// seedWallet inserts a wallet and records its opening balance for the
// invariant checker.
func (e *testEnv) seedWallet(id string, balance int64, currency string) {
	e.t.Helper()
	now := sqlrepo.FormatTime(time.Now())
	require.NoError(e.t, e.exec(
		"INSERT INTO wallets (id, balance, currency, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		id, balance, currency, now, now))
	e.mu.Lock()
	e.opening[id] = balance
	e.mu.Unlock()
}

var errRollback = errors.New("test: roll back")

// inRolledBackTx runs fn inside a real transaction and then rolls it back.
// Repository tests use it to exercise writes without leaving half-finished
// state (e.g. PENDING transfers) for the invariant checker.
func (e *testEnv) inRolledBackTx(fn func(r service.Repos)) {
	e.t.Helper()
	err := e.store.TxManager.WithinTx(context.Background(), func(r service.Repos) error {
		fn(r)
		return errRollback
	})
	require.ErrorIs(e.t, err, errRollback)
}

// insertProcessedTransfer writes a complete, consistent PROCESSED transfer
// with raw SQL: the transfer row, both ledger entries and both balance
// updates. Constraint tests use it as a valid starting point.
func (e *testEnv) insertProcessedTransfer(from, to string, amount int64) string {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	now := sqlrepo.FormatTime(time.Now())
	require.NoError(e.t, e.exec(
		`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'PROCESSED', NULL, ?, ?)`,
		id, "key-"+id, from, to, amount, now, now))
	require.NoError(e.t, e.insertLedgerEntry(id, from, "DEBIT", amount))
	require.NoError(e.t, e.insertLedgerEntry(id, to, "CREDIT", amount))
	require.NoError(e.t, e.exec("UPDATE wallets SET balance = balance - ? WHERE id = ?", amount, from))
	require.NoError(e.t, e.exec("UPDATE wallets SET balance = balance + ? WHERE id = ?", amount, to))
	return id
}

func (e *testEnv) insertLedgerEntry(transferID, walletID, entryType string, amount int64) error {
	return e.exec(
		`INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.Must(uuid.NewV7()).String(), transferID, walletID, entryType, amount, sqlrepo.FormatTime(time.Now()))
}

// newTransfer builds a PENDING domain transfer for repository tests.
func newTransfer(t *testing.T, key, from, to string, amount int64, at time.Time) *domain.Transfer {
	t.Helper()
	tr, err := domain.NewTransfer(key, from, to, amount, at)
	require.NoError(t, err)
	return tr
}

// transferService returns a TransferService backed by env's database.
func (e *testEnv) transferService() *service.TransferService {
	return service.NewTransferService(e.store.TxManager)
}

// walletService returns a WalletService backed by env's database.
func (e *testEnv) walletService() *service.WalletService {
	return service.NewWalletService(e.store.Repos)
}

// transfer calls CreateTransfer with a fresh context.
func (e *testEnv) transfer(key, from, to string, amount int64) (service.TransferResult, error) {
	return e.transferService().CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount,
	})
}

// assertNothingPersisted checks that a rejected request left no trace.
func (e *testEnv) assertNothingPersisted() {
	e.t.Helper()
	require.Equal(e.t, int64(0), e.count("transfers"), "transfers")
	require.Equal(e.t, int64(0), e.count("ledger_entries"), "ledger_entries")
	require.Equal(e.t, int64(0), e.count("idempotency_records"), "idempotency_records")
}
