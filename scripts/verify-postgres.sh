#!/usr/bin/env bash
# End-to-end verification against a real PostgreSQL. See docs/postgres.md.
#
#   make verify-pg                                  # uses the docker-compose Postgres
#   PG_ADMIN_DSN='postgres://me@localhost:5432/postgres?sslmode=disable' ./scripts/verify-postgres.sh
#
# PG_ADMIN_DSN must be able to CREATE DATABASE. The script creates (and
# recreates on every run) two databases next to it:
#   wallet_verify       - used by the real server
#   wallet_verify_test  - used by the test suite (one schema per test)
#
# Steps:
#   1. connectivity         psql can reach Postgres
#   2. test suite           go test -race ./... with TEST_POSTGRES_DSN; Postgres subtests must run
#   3. real server          built binary started with DB_DRIVER=postgres; /readyz must be 200
#   4. demo flows           scripts/demo.sh through that server
#   5. SQL invariants       checked directly in Postgres
#   6. graceful shutdown    SIGTERM; the server must log "server stopped"
#
# Requires: go, psql, curl.
set -euo pipefail

PG_ADMIN_DSN="${PG_ADMIN_DSN:-postgres://wallet:wallet@localhost:5432/postgres?sslmode=disable}"
PORT="${VERIFY_PORT:-18090}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
server_pid=""

cleanup() {
  if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then kill "$server_pid" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

step() { printf '\n\033[1m[%s] %s\033[0m\n' "$1" "$2"; }
pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; exit 1; }

# with_db DSN NAME: the same DSN pointing at database NAME.
with_db() { printf '%s' "$1" | sed -E "s#^(postgres(ql)?://[^/]+/)[^?]*#\1$2#"; }

SERVER_DSN="$(with_db "$PG_ADMIN_DSN" wallet_verify)"
TEST_DSN="$(with_db "$PG_ADMIN_DSN" wallet_verify_test)"
cd "$ROOT"

step 1 "Connectivity"
version="$(psql "$PG_ADMIN_DSN" -tAc 'select version()' 2>"$tmp/psql.err")" \
  || fail "cannot connect with PG_ADMIN_DSN: $(cat "$tmp/psql.err")"
pass "connected: ${version%% on *}"
for db in wallet_verify wallet_verify_test; do
  psql "$PG_ADMIN_DSN" -qc "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" >/dev/null 2>&1 \
    || fail "could not (re)create database $db (does the user have CREATEDB?)"
done
pass "fresh databases wallet_verify and wallet_verify_test"

step 2 "Test suite on Postgres (go test -race ./...)"
if ! TEST_POSTGRES_DSN="$TEST_DSN" go test -race -count=1 -v ./... >"$tmp/test.log" 2>&1; then
  grep -E -- '--- FAIL|^FAIL' "$tmp/test.log" | head -20
  fail "test suite failed (full log above)"
fi
pg_pass="$(grep -cE -- '--- PASS: .*/postgres|--- PASS: TestRun_PostgresEndToEnd' "$tmp/test.log" || true)"
pg_skip="$(grep -cE -- '--- SKIP: .*/postgres|--- SKIP: TestRun_PostgresEndToEnd' "$tmp/test.log" || true)"
[ "$pg_pass" -gt 0 ] || fail "no Postgres subtests ran"
[ "$pg_skip" -eq 0 ] || fail "$pg_skip Postgres tests were skipped"
pass "all packages ok; $pg_pass Postgres tests passed, 0 skipped"

step 3 "Real server with DB_DRIVER=postgres"
go build -o "$tmp/server" ./cmd/server
DB_DRIVER=postgres DB_DSN="$SERVER_DSN" PORT="$PORT" "$tmp/server" >"$tmp/server.log" 2>&1 &
server_pid=$!
for _ in $(seq 1 50); do
  curl -fsS "http://localhost:$PORT/readyz" >/dev/null 2>&1 && break
  kill -0 "$server_pid" 2>/dev/null || { cat "$tmp/server.log"; fail "server exited during startup"; }
  sleep 0.2
done
[ "$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$PORT/readyz")" = 200 ] || fail "/readyz is not 200"
grep -q '"db_driver":"postgres"' "$tmp/server.log" || fail "server did not report db_driver=postgres"
pass "server up, /readyz 200 (the DB ping succeeded), db_driver=postgres"

step 4 "Demo flows through the server"
BASE_URL="http://localhost:$PORT" ./scripts/demo.sh >"$tmp/demo.log" 2>&1 || { cat "$tmp/demo.log"; fail "demo.sh failed"; }
for expect in "201 Created" "Idempotent-Replayed: true" "409 Conflict" "422 Unprocessable Entity" "404 Not Found" "400 Bad Request"; do
  grep -q "$expect" "$tmp/demo.log" || fail "demo output is missing: $expect"
done
grep -q 'replayed responses: 19 of 20' "$tmp/demo.log" || fail "same-key burst did not replay 19 of 20"
burst="$(grep -E 'expected exactly [0-9]+ x 201' "$tmp/demo.log")"
want="$(printf '%s' "$burst" | sed -E 's/.*expected exactly ([0-9]+) x 201.*/\1/')"
grep -qE "status codes: .*${want} x 201" "$tmp/demo.log" || fail "burst: expected $want x 201 ($(grep 'status codes' "$tmp/demo.log"))"
pass "201, replay, 409, 422 (x2), 404, 400, same-key burst (19/20 replayed), drain burst ($want x 201 as expected)"

step 5 "SQL invariants, checked directly in Postgres"
psql "$SERVER_DSN" -tA -F '|' >"$tmp/checks" <<'SQL'
SELECT 'migrations applied (schema + seed)', COUNT(*) = 2 FROM schema_migrations
UNION ALL SELECT 'money conserved (seed total 160000)', SUM(balance) = 160000 FROM wallets
UNION ALL SELECT 'no negative balances', MIN(balance) >= 0 FROM wallets
UNION ALL SELECT 'no PENDING transfers', COUNT(*) = 0 FROM transfers WHERE status = 'PENDING'
UNION ALL SELECT 'ledger rows = 2 x PROCESSED',
  (SELECT COUNT(*) FROM ledger_entries) = 2 * (SELECT COUNT(*) FROM transfers WHERE status = 'PROCESSED')
UNION ALL SELECT 'FAILED transfers have no ledger rows',
  NOT EXISTS (SELECT 1 FROM ledger_entries l JOIN transfers t ON t.id = l.transfer_id WHERE t.status = 'FAILED')
UNION ALL SELECT 'each PROCESSED has DEBIT(from) + CREDIT(to) of its amount',
  NOT EXISTS (SELECT 1 FROM transfers t WHERE t.status = 'PROCESSED' AND (
    SELECT COUNT(*) FROM ledger_entries l WHERE l.transfer_id = t.id AND l.amount = t.amount AND (
      (l.entry_type = 'DEBIT' AND l.wallet_id = t.from_wallet_id) OR
      (l.entry_type = 'CREDIT' AND l.wallet_id = t.to_wallet_id))) <> 2)
UNION ALL SELECT 'balance = seed + credits - debits, per wallet',
  NOT EXISTS (SELECT 1 FROM wallets w WHERE w.balance <>
    (CASE w.id WHEN 'wallet_1' THEN 100000 WHEN 'wallet_2' THEN 50000 WHEN 'wallet_3' THEN 0 WHEN 'wallet_usd' THEN 10000 END)
    + COALESCE((SELECT SUM(CASE l.entry_type WHEN 'CREDIT' THEN l.amount ELSE -l.amount END)
                FROM ledger_entries l WHERE l.wallet_id = w.id), 0))
UNION ALL SELECT 'every idempotency key linked to its transfer', COUNT(*) = 0 FROM idempotency_records WHERE transfer_id IS NULL
UNION ALL SELECT '404 request left no rows', COUNT(*) = 0 FROM transfers WHERE to_wallet_id = 'wallet_404'
UNION ALL SELECT 'same-key burst produced exactly one transfer', COUNT(*) = 1 FROM transfers WHERE idempotency_key LIKE '%-burst';
SQL
failed=0
while IFS='|' read -r name ok; do
  if [ "$ok" = "t" ]; then pass "$name"; else printf '  \033[31mFAIL\033[0m %s\n' "$name"; failed=1; fi
done <"$tmp/checks"
[ "$failed" -eq 0 ] || fail "SQL invariants violated"

step 6 "Graceful shutdown"
kill -TERM "$server_pid"
wait "$server_pid" || true
server_pid=""
grep -q '"msg":"server stopped"' "$tmp/server.log" || { tail -5 "$tmp/server.log"; fail "no clean shutdown"; }
pass "SIGTERM -> in-flight requests drained -> \"server stopped\""

printf '\n\033[32;1mPostgres end-to-end verification passed.\033[0m\n'
