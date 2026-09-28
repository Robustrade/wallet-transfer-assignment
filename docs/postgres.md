# PostgreSQL: setup, connection and end-to-end verification

The service runs on SQLite by default. This page covers PostgreSQL: how to start one, how the
service connects, and how to prove the whole flow works end to end. The design reasons (locking,
isolation, retries) are in [design.md §7](design.md#7-concurrency).

## Verification status

| What | Result |
|---|---|
| Full test suite with `TEST_POSTGRES_DSN` (`go test -race ./...`) | all packages pass; **80 Postgres tests passed, 0 skipped** |
| Real server binary with `DB_DRIVER=postgres`, all demo flows, SQL invariants, graceful shutdown (`make verify-pg`) | **all 6 steps pass** |
| Tested against | PostgreSQL 14.20, local (Homebrew), 2026-09-28 |
| `docker-compose.yml` Postgres (16-alpine) | not yet run: Docker was unavailable on the development machine |

## 1. Start a PostgreSQL

Pick one.

**A. Docker (matches the Makefile defaults)**

```sh
docker compose up -d --wait postgres
# user wallet / password wallet / database wallet, on localhost:5432
```

**B. Homebrew**

```sh
brew services start postgresql@16
createuser -s wallet          # local superuser without a password
createdb -O wallet wallet
```

With Homebrew there is no password, so leave it out of the DSN: `postgres://wallet@localhost:5432/wallet?sslmode=disable`.

**C. Any existing server.** You need a database, and a user that can create tables in it. To run
`make verify-pg`, that user must also be able to `CREATE DATABASE`.

Check that it is reachable:

```sh
psql 'postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable' -c 'select version();'
```

## 2. Run the service on it

```sh
make run-pg     # Docker option: starts the container, then the server

# or any server:
DB_DRIVER=postgres DB_DSN='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable' make run
```

At startup the server:

1. validates config. `DB_DRIVER=postgres` without `DB_DSN` is a startup error;
2. connects and **pings**. An unreachable database is a startup error, not a failure at the first request;
3. applies the embedded migrations it has not applied yet (`0001_init.sql`, `0002_demo_seed.sql`),
   tracked in `schema_migrations`;
4. logs `{"msg":"server listening","db_driver":"postgres",...}`. The DSN is never logged.

`GET /readyz` pings the database: `{"status":"ready"}` means the server can reach Postgres.

## 3. How the connection works

| Aspect | Implementation | Where |
|---|---|---|
| Driver | `github.com/jackc/pgx/v5/stdlib` through `database/sql` | `internal/repository/postgres/open.go` |
| Pool | at most 20 connections (and 20 idle); excess requests wait, bounded by the request deadline | same |
| Isolation | `READ COMMITTED` on every transaction | `postgres/dialect.go` `TxOptions` |
| Lock wait | `set_config('lock_timeout', '<LOCK_TIMEOUT>ms', true)` at the start of every transaction (equivalent to `SET LOCAL`, so it never leaks into pooled connections) | `SetupTx` |
| Row locks | `SELECT … FOR UPDATE` on both wallets, in sorted id order | `sqlrepo/wallet_repo.go`, `service/transfer_service.go` |
| Balance write | compare-and-set `… WHERE id = ? AND balance = <value read under the lock>`; a mismatch is `ErrStaleWrite` | `sqlrepo/wallet_repo.go` `UpdateBalance` |
| History query | two index-ordered branches merged with `UNION ALL`: 2 × Index Scan Backward instead of BitmapOr plus a sort (0.18 ms vs 31.8 ms at 60k transfers for one wallet) | `sqlrepo/transfer_repo.go` `ListByWallet` |
| Placeholders | SQL written once with `?`, rewritten to `$1, $2, …` | `Rebind` |
| Error classification | `40P01`/`40001` → retry the whole transaction (3 attempts); `55P03` (lock_timeout) → 503, no retry; `23505` → unique violation | `IsRetryable` / `IsLockTimeout` / `IsUniqueViolation` |
| Types | `BIGINT` money, `UUID` ids, `TIMESTAMPTZ`; timestamps bound as UTC strings with microseconds | `postgres/migrations/0001_init.sql`, `sqlrepo/repos.go` |

## 4. Verify end to end

### One command

```sh
make verify-pg                                      # docker-compose Postgres
make verify-pg PG_ADMIN_DSN='postgres://wallet@localhost:5432/postgres?sslmode=disable'   # any other
```

`PG_ADMIN_DSN` must be allowed to `CREATE DATABASE`. The script (re)creates `wallet_verify` and
`wallet_verify_test` next to it, and never touches your other databases.

| Step | What it proves |
|---|---|
| 1. Connectivity | `psql` reaches the server; fresh databases are created |
| 2. Test suite | `go test -race ./...` passes with `TEST_POSTGRES_DSN`, and the Postgres tests really ran (non-zero count, zero skipped) |
| 3. Real server | the built binary starts with `DB_DRIVER=postgres`, logs `db_driver=postgres`, and `/readyz` is 200 |
| 4. Demo flows | `scripts/demo.sh` through that server: 201, replay (`Idempotent-Replayed`), 409, 422 ×2, 404, 400; 20 same-key requests replay 19; a 30-way drain processes exactly `balance / 1000` |
| 5. SQL invariants | 11 checks run **directly in Postgres**: migrations applied, money conserved, no negative balances, no PENDING, ledger shape per status, balance = seed + credits − debits, every key linked, the 404 left no rows, the burst produced one transfer |
| 6. Graceful shutdown | SIGTERM drains in-flight requests and logs `server stopped` |

Each step prints PASS or FAIL, and the script exits non-zero on the first failure.

Sample output:

```
[1] Connectivity
  PASS connected: PostgreSQL 14.20 (Homebrew)
[2] Test suite on Postgres (go test -race ./...)
  PASS all packages ok; 80 Postgres tests passed, 0 skipped
[3] Real server with DB_DRIVER=postgres
  PASS server up, /readyz 200 (the DB ping succeeded), db_driver=postgres
[4] Demo flows through the server
  PASS 201, replay, 409, 422 (x2), 404, 400, same-key burst (19/20 replayed), drain burst (5 x 201 as expected)
[5] SQL invariants, checked directly in Postgres
  PASS money conserved (seed total 160000)
  ... (11 checks)
[6] Graceful shutdown
  PASS SIGTERM -> in-flight requests drained -> "server stopped"
Postgres end-to-end verification passed.
```

### Just the tests

```sh
make test-pg                                                           # docker-compose Postgres
TEST_POSTGRES_DSN='postgres://wallet@localhost:5432/wallet?sslmode=disable' go test -race -count=1 ./...
```

- Without `TEST_POSTGRES_DSN`, only SQLite runs, and the Postgres-only tests report SKIP.
- With it, **every integration test runs on both drivers**. Each test gets its own randomly named
  schema (via `search_path`), dropped afterwards, so tests run in parallel and leave nothing behind.
- To confirm Postgres actually ran:
  `go test -count=1 -v ./tests/integration/ | grep -c -- '--- PASS.*/postgres'` must be non-zero.

### What runs on Postgres

| Level | Tests |
|---|---|
| Binary end to end | `cmd/server` `TestRun_PostgresEndToEnd`: boots the real server with `DB_DRIVER=postgres`, then over real HTTP: 201, replay, 409, 422 ×2, 404, 400, wallet balance, graceful shutdown; then checks rows in SQL |
| HTTP end to end | `TestHTTP_EndToEnd`, `TestHTTP_DatabaseClosed` (real router, real database) |
| Service | transfers, all FAILED rules, 404 leaves nothing, validation, idempotency (replay, 409, FAILED replayed after a top-up, key reusable after 404, case-sensitive keys), cancellation before commit |
| Concurrency | 50-way double-spend, 20× same key, same key with different amounts, 100 opposite-direction transfers (no deadlock), 500 random transfers, reads during writes |
| Transactions | TxManager commit/rollback, whole-transaction retry, retry limit → 503, context cancellation, **real `lock_timeout`** (another connection holds `FOR UPDATE`) |
| Database constraints | raw SQL, bypassing the app: negative balance, third ledger row, self-transfer, zero amount, invalid status or reason, duplicate keys, foreign keys |
| Repositories | every port method, including the guarded status update |
| Dialect (unit) | placeholder rebinding, SQLSTATE classification |

After every integration test an invariant checker verifies: money conserved, ledger shape per
status, balance = opening + credits − debits, no PENDING left, every idempotency key linked.

### Manual checks

For a full curl walkthrough with the expected response of every step, see
[manual-testing.md](manual-testing.md). Quick checks with the server running on Postgres:

```sh
curl localhost:8080/readyz
make demo
psql 'postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
```

```sql
-- the app's connections (while the server runs)
SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid();

SELECT * FROM schema_migrations;
SELECT id, balance, currency FROM wallets ORDER BY id;
SELECT status, count(*) FROM transfers GROUP BY status;
SELECT sum(balance) FROM wallets;                          -- demo seed total: 160000

-- the latest transfers with their ledger lines
SELECT t.idempotency_key, t.status, l.entry_type, l.wallet_id, l.amount
FROM transfers t LEFT JOIN ledger_entries l ON l.transfer_id = t.id
ORDER BY t.created_at DESC LIMIT 10;

-- the database protects itself
UPDATE wallets SET balance = -1 WHERE id = 'wallet_1';     -- ERROR: violates check constraint
```

## 5. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `connection refused` | Postgres is not running, or is on another port. Check with `psql '<dsn>' -c 'select 1'` |
| `password authentication failed` | Wrong credentials. Docker uses `wallet:wallet`; Homebrew users usually have no password |
| `database "wallet" does not exist` | `createdb wallet`, or use the Docker option |
| `DB_DSN is required when DB_DRIVER is postgres` | Set `DB_DSN` |
| `/readyz` returns 503 | The server started, but the database became unreachable afterwards |
| Postgres tests show SKIP | `TEST_POSTGRES_DSN` is not set in that shell |
| `TEST_POSTGRES_DSN is set but Postgres is unreachable` | The DSN is set but wrong. The suite fails fast instead of silently skipping |
| `permission denied to create database` (verify-pg) | Use a `PG_ADMIN_DSN` user with `CREATEDB`, or run `make test-pg` / the manual checks instead |
| 503 `TEMPORARILY_UNAVAILABLE` under load | Lock wait exceeded `LOCK_TIMEOUT`, or the request exceeded `REQUEST_TIMEOUT`. Nothing was committed; retry with the same key |
