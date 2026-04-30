//go:build integration

// Package integration holds end-to-end tests that exercise the service against
// a real Postgres instance. These tests run only when the `integration` build
// tag is set and require the INTEGRATION_DATABASE_URL environment variable.
//
// SCOPE NOTE — for the assignment scope, INTEGRATION_DATABASE_URL defaults to
// the same DSN as DATABASE_URL (see Makefile). That is, the integration tests
// share the dev Postgres instance used by `make run` and `make seed`. Each
// test starts with `TRUNCATE`, so running `make test-int` will wipe any
// interactively-seeded rows. A production-grade setup would point this at a
// dedicated test database (or testcontainers); we intentionally don't, to
// keep the local-run story to a single Postgres.
//
// IMPORTANT: tests in this package share a single Postgres schema and rely on
// `TRUNCATE` in setupTestEnv to start each test with a clean slate. Therefore
// these tests MUST NOT call t.Parallel() — running them in parallel would let
// one test's TRUNCATE wipe another test's in-flight rows. Run them
// sequentially (the default).
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // registers the file:// migrations source
	"github.com/gorilla/mux"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" sql.DB driver for golang-migrate
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Robustrade/wallet-transfer-assignment/config/infra"
	transferinit "github.com/Robustrade/wallet-transfer-assignment/services/transfer/init"
)

const envDatabaseURL = "INTEGRATION_DATABASE_URL"

// transferReq is the JSON body posted to /transfers in the integration tests.
// Kept separate from the production DTO so the tests exercise the wire
// contract directly.
type transferReq struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// transferResp is the JSON body returned by /transfers.
type transferResp struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	FailureReason  *string   `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Replayed       bool      `json:"replayed"`
}

// errorResp is the JSON body returned for error responses.
type errorResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// testEnv bundles everything an integration test needs.
type testEnv struct {
	t      *testing.T
	DB     *gorm.DB
	sqlDB  *sql.DB
	Server *httptest.Server
	client *http.Client
}

// setupTestEnv connects to the configured Postgres, applies migrations,
// truncates tables to start fresh, builds the HTTP test server, and returns
// the env. The test is skipped if INTEGRATION_DATABASE_URL is unset.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dsn := os.Getenv(envDatabaseURL)
	if dsn == "" {
		t.Skipf("set %s to run integration tests", envDatabaseURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := infra.InitPostgres(ctx, infra.PostgresConfig{DSN: dsn})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)

	require.NoError(t, applyMigrations(sqlDB))
	truncateAll(t, sqlDB)

	router := mux.NewRouter()
	require.NoError(t, transferinit.InitTransferService(router, transferinit.Config{
		DB:     db,
		Logger: infra.InitLogger(infra.LoggerConfig{Level: "warn"}),
	}))

	server := httptest.NewServer(router)

	t.Cleanup(func() {
		server.Close()
		_ = sqlDB.Close()
	})

	// Per-call timeout so a deadlocked handler fails the test fast instead of
	// hanging until `go test -timeout` fires. The transfer flow is bounded by
	// a couple of round-trips to Postgres; 10s is comfortably above the 99p.
	client := &http.Client{Timeout: 10 * time.Second}

	return &testEnv{t: t, DB: db, sqlDB: sqlDB, Server: server, client: client}
}

// applyMigrations brings the database up to the latest version.
func applyMigrations(db *sql.DB) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("migrations: driver: %w", err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	migrationsPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	abs, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("migrations: abspath: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+abs, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrations: new: %w", err)
	}
	// Don't call m.Close(). golang-migrate's Postgres driver closes the
	// underlying *sql.DB when its Close() runs, even though we passed the
	// connection in via WithInstance and still own it. The caller continues
	// to use db after this returns; closing it here would surface as
	// "sql: database is closed" on the next query. The migrate instance and
	// the file-source goroutine are GC'd when this function returns.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}

// truncateAll wipes all tables. Used to start each test with a clean slate.
func truncateAll(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec("TRUNCATE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE")
	require.NoError(t, err)
}

// seedWallet inserts a wallet with the given balance.
func (e *testEnv) seedWallet(id, currency string, balanceMinor int64) {
	e.t.Helper()
	_, err := e.sqlDB.Exec(
		`INSERT INTO wallets (id, balance_minor, currency) VALUES ($1, $2, $3)`,
		id, balanceMinor, currency,
	)
	require.NoError(e.t, err)
}

// walletBalance reads the balance of a wallet.
func (e *testEnv) walletBalance(id string) int64 {
	e.t.Helper()
	var b int64
	require.NoError(e.t,
		e.sqlDB.QueryRow(`SELECT balance_minor FROM wallets WHERE id = $1`, id).Scan(&b),
	)
	return b
}

// transferCount returns the number of transfer rows.
func (e *testEnv) transferCount() int {
	e.t.Helper()
	var n int
	require.NoError(e.t,
		e.sqlDB.QueryRow(`SELECT COUNT(*) FROM transfers`).Scan(&n),
	)
	return n
}

// ledgerCount returns the number of ledger_entries rows.
func (e *testEnv) ledgerCount() int {
	e.t.Helper()
	var n int
	require.NoError(e.t,
		e.sqlDB.QueryRow(`SELECT COUNT(*) FROM ledger_entries`).Scan(&n),
	)
	return n
}

// ledgerSumByType returns SUM(amount_minor) for the given entry type.
func (e *testEnv) ledgerSumByType(entryType string) int64 {
	e.t.Helper()
	var s sql.NullInt64
	require.NoError(e.t,
		e.sqlDB.QueryRow(
			`SELECT COALESCE(SUM(amount_minor), 0) FROM ledger_entries WHERE entry_type = $1`,
			entryType,
		).Scan(&s),
	)
	return s.Int64
}

// ledgerNetForWallet returns the net (credits - debits) for a wallet.
func (e *testEnv) ledgerNetForWallet(walletID string) int64 {
	e.t.Helper()
	var net sql.NullInt64
	require.NoError(e.t,
		e.sqlDB.QueryRow(
			`SELECT COALESCE(SUM(
				CASE WHEN entry_type = 'CREDIT' THEN amount_minor
				     WHEN entry_type = 'DEBIT'  THEN -amount_minor
				END
			), 0)
			 FROM ledger_entries WHERE wallet_id = $1`,
			walletID,
		).Scan(&net),
	)
	return net.Int64
}

// transferStatus returns the status of the single transfer with the given key.
func (e *testEnv) transferStatusByKey(key string) string {
	e.t.Helper()
	var s string
	require.NoError(e.t,
		e.sqlDB.QueryRow(`SELECT status FROM transfers WHERE idempotency_key = $1`, key).Scan(&s),
	)
	return s
}

// callTransfer issues a POST /transfers with the supplied body and returns
// (status, body, transport-error). Goroutine-safe: callers in concurrency
// tests inspect the error directly rather than failing the test from a
// non-main goroutine.
func (e *testEnv) callTransfer(req transferReq) (int, []byte, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, e.Server.URL+"/transfers", bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

// post is the sequential-test convenience wrapper around callTransfer that
// fails the test on transport errors via require.NoError.
func (e *testEnv) post(t *testing.T, body transferReq) (int, []byte) {
	t.Helper()
	status, raw, err := e.callTransfer(body)
	require.NoError(t, err)
	return status, raw
}

// transferCurrencies returns the currency column for every transfer row.
// Used to assert the canonical-currency-on-row invariant in the I3 test.
func (e *testEnv) transferCurrencies() []string {
	e.t.Helper()
	rows, err := e.sqlDB.Query(`SELECT currency FROM transfers ORDER BY created_at`)
	require.NoError(e.t, err)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var c string
		require.NoError(e.t, rows.Scan(&c))
		out = append(out, c)
	}
	require.NoError(e.t, rows.Err())
	return out
}
