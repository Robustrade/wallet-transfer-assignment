package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // "pgx" driver, for schema setup and SQL checks
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_PostgresEndToEnd boots the real server with DB_DRIVER=postgres and
// drives it over real HTTP. It is the same binary path `make run-pg` uses:
// config, the pgx driver, migrations, seed, router, services, and graceful
// shutdown. It then checks the resulting rows directly in Postgres.
//
// It runs only when TEST_POSTGRES_DSN is set. The server gets a throwaway
// schema (via search_path), so the target database is left untouched.
func TestRun_PostgresEndToEnd(t *testing.T) {
	adminDSN := os.Getenv("TEST_POSTGRES_DSN")
	if adminDSN == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	t.Parallel()

	admin, err := sql.Open("pgx", adminDSN)
	require.NoError(t, err)
	defer admin.Close()
	require.NoError(t, admin.Ping(), "Postgres must be reachable at TEST_POSTGRES_DSN")

	schema := randomSchema(t)
	_, err = admin.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })

	u, err := url.Parse(adminDSN)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	// Boot the server exactly as main() does, on a free port.
	base, stop := startServer(t, map[string]string{
		"DB_DRIVER": "postgres",
		"DB_DSN":    u.String(),
	})

	// readyz pings the database, so 200 proves the server reached Postgres.
	status, _, _ := send(t, "GET", base+"/readyz", "")
	require.Equal(t, 200, status)

	body := func(key, from, to string, amount int) string {
		return fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":%d}`, key, from, to, amount)
	}

	// Every transfer outcome, against the demo seed.
	status, first, _ := send(t, "POST", base+"/transfers", body("pg-1", "wallet_1", "wallet_2", 1000))
	require.Equal(t, 201, status, first)

	status, replay, hdr := send(t, "POST", base+"/transfers", body("pg-1", "wallet_1", "wallet_2", 1000))
	assert.Equal(t, 201, status)
	assert.Equal(t, "true", hdr.Get("Idempotent-Replayed"))
	assert.JSONEq(t, first, replay, "replay returns the original body")

	status, _, _ = send(t, "POST", base+"/transfers", body("pg-1", "wallet_1", "wallet_2", 9999))
	assert.Equal(t, 409, status, "same key, different payload")

	status, failed, _ := send(t, "POST", base+"/transfers", body("pg-2", "wallet_3", "wallet_1", 500))
	assert.Equal(t, 422, status)
	assert.Contains(t, failed, `"INSUFFICIENT_FUNDS"`)

	status, mismatch, _ := send(t, "POST", base+"/transfers", body("pg-3", "wallet_1", "wallet_usd", 100))
	assert.Equal(t, 422, status)
	assert.Contains(t, mismatch, `"CURRENCY_MISMATCH"`)

	status, _, _ = send(t, "POST", base+"/transfers", body("pg-4", "wallet_1", "wallet_404", 100))
	assert.Equal(t, 404, status)

	status, _, _ = send(t, "POST", base+"/transfers", `{"idempotencyKey":"pg-5","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10.5}`)
	assert.Equal(t, 400, status)

	status, wallet, _ := send(t, "GET", base+"/wallets/wallet_1", "")
	require.Equal(t, 200, status)
	var w struct{ Balance int64 }
	require.NoError(t, json.Unmarshal([]byte(wallet), &w))
	assert.Equal(t, int64(99000), w.Balance)

	require.NoError(t, stop(), "graceful shutdown")

	// Check the rows the server wrote, straight from Postgres.
	count := func(query string) int {
		var n int
		require.NoError(t, admin.QueryRow(fmt.Sprintf(query, schema)).Scan(&n), query)
		return n
	}
	assert.Equal(t, 2, count("SELECT COUNT(*) FROM %s.schema_migrations"), "schema + seed migrations applied")
	assert.Equal(t, 160000, count("SELECT SUM(balance) FROM %s.wallets"), "money conserved")
	assert.Equal(t, 3, count("SELECT COUNT(*) FROM %s.transfers"), "1 PROCESSED + 2 FAILED; 404/400/409 stored nothing")
	assert.Equal(t, 1, count("SELECT COUNT(*) FROM %s.transfers WHERE status = 'PROCESSED'"))
	assert.Equal(t, 2, count("SELECT COUNT(*) FROM %s.ledger_entries"), "one DEBIT + one CREDIT")
	assert.Equal(t, 0, count("SELECT COUNT(*) FROM %s.transfers WHERE status = 'PENDING'"))
	assert.Equal(t, 0, count("SELECT COUNT(*) FROM %s.idempotency_records WHERE transfer_id IS NULL"))
}

func randomSchema(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "e2e_" + hex.EncodeToString(b)
}

// send performs an HTTP request and returns status, body and headers.
func send(t *testing.T, method, url, body string) (int, string, http.Header) {
	t.Helper()
	resp, err := request(method, url, body)
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode, string(b), resp.Header
}
