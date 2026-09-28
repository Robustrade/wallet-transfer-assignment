# Wallet Transfer Service

A Go service exposing `POST /transfers` to move money between wallets with:

- **exactly-once semantics** per `idempotencyKey` (retries replay the original result)
- a **double-entry ledger**: exactly one DEBIT and one CREDIT per processed transfer
- **correct balances under concurrency**: no double spend, never negative
- **atomic** transfers: everything commits in one transaction, or nothing does
- a **safe state machine**: `PENDING → PROCESSED | FAILED`, enforced in code and by a guarded `UPDATE`
- a **switchable database**: SQLite (default, zero setup) or PostgreSQL

The full design (contract, schema, idempotency, locking per driver, failure modes, tradeoffs) is in
[`docs/design.md`](docs/design.md). The original brief is in [`ASSIGNMENT.md`](ASSIGNMENT.md).

---

## How to run and check it

Follow the steps in order. Each one says what you should see.

### Prerequisites

| Needed for | Tool | Check |
|---|---|---|
| everything | **Go 1.23+** | `go version` |
| the curl checks | **curl** | `curl --version` |
| the PostgreSQL steps only | **Docker** *or* a local PostgreSQL, plus **psql** | `docker version` / `psql --version` |

SQLite needs nothing: the driver is pure Go and compiled into the binary.

### Step 1: Run the tests (SQLite)

```sh
make test          # = go test -race -count=1 ./...
```

**Expect** every package to report `ok` (two report `[no test files]`) and no `FAIL`:

```
ok   github.com/LokilokeshM/wallet-transfer-assignment/cmd/server
ok   github.com/LokilokeshM/wallet-transfer-assignment/internal/domain
...
ok   github.com/LokilokeshM/wallet-transfer-assignment/tests/integration
```

### Step 2: Start the server (SQLite)

In **terminal 1**:

```sh
rm -f wallet.db wallet.db-wal wallet.db-shm   # optional: start from a fresh database
make run
```

**Expect** a JSON log line containing `"msg":"server listening"` and `"db_driver":"sqlite"`. The
server creates `./wallet.db`, applies the migrations and seeds four demo wallets:

| Wallet | Balance | Currency |
|---|---|---|
| `wallet_1` | 100000 | SGD |
| `wallet_2` | 50000 | SGD |
| `wallet_3` | 0 | SGD |
| `wallet_usd` | 10000 | USD |

Amounts are integer minor units, so 100000 means 1000.00. Leave the server running. It prints one
log line per request.

### Step 3: Check every behavior with curl

In **terminal 2**, run these in order. `-i` shows the status line and headers.

```sh
# health: 200 {"status":"ok"} and 200 {"status":"ready"}
curl -i localhost:8080/healthz
curl -i localhost:8080/readyz

# 1. normal transfer -> 201, "status":"PROCESSED", "failureReason":null   (note the "id")
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":500}'

# 2. same request again -> 201 + header "Idempotent-Replayed: true", SAME id, no money moves
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":500}'

# 3. same key, different amount -> 409 "IDEMPOTENCY_KEY_REUSED"
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":999}'

# 4. insufficient funds (wallet_3 has 0) -> 422, "status":"FAILED", "failureReason":"INSUFFICIENT_FUNDS"
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-2","fromWalletId":"wallet_3","toWalletId":"wallet_1","amount":100}'

# 5. currency mismatch (SGD -> USD) -> 422, "failureReason":"CURRENCY_MISMATCH"
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-3","fromWalletId":"wallet_1","toWalletId":"wallet_usd","amount":100}'

# 6. unknown wallet -> 404 "WALLET_NOT_FOUND" (nothing is stored)
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-4","fromWalletId":"wallet_1","toWalletId":"nobody","amount":100}'

# 7. invalid input -> 400 "VALIDATION_ERROR", message "amount: must be an integer number of minor units"
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"my-test-5","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10.5}'

# 8. your request id is echoed -> response header "X-Request-Id: my-trace-123"
curl -i localhost:8080/healthz -H 'X-Request-ID: my-trace-123'; echo

# 9. 10 identical requests at the same time -> prints "Idempotent-Replayed: true" 9 times (one transfer)
for i in $(seq 1 10); do
  curl -s -o /dev/null -D - -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
    -d '{"idempotencyKey":"my-burst-1","fromWalletId":"wallet_2","toWalletId":"wallet_3","amount":50}' \
    | grep -i 'replayed' &
done; wait

# 10. final balances and history
curl localhost:8080/wallets/wallet_1
curl localhost:8080/wallets/wallet_2
curl localhost:8080/wallets/wallet_3
curl 'localhost:8080/wallets/wallet_1/transfers?limit=5'
```

**Expected final balances** on a fresh database. Only step 1 and the burst in step 9 moved money:

| Wallet | Balance | Why |
|---|---|---|
| `wallet_1` | **99500** | 100000 − 500 (step 1) |
| `wallet_2` | **50450** | 50000 + 500 (step 1) − 50 (step 9) |
| `wallet_3` | **50** | + 50 (step 9); the FAILED transfer in step 4 moved nothing |
| `wallet_usd` | **10000** | the FAILED transfer in step 5 moved nothing |

The history for `wallet_1` shows 3 transfers, newest first: steps 5, 4 and 1.

- Each step's full response body, and more validation cases, are in [docs/manual-testing.md](docs/manual-testing.md).
- The same requests are clickable in [`requests.http`](requests.http) (VS Code REST Client / JetBrains HTTP Client).
- Or run everything at once with `make demo`: it runs every scenario plus a parallel burst and prints the balances.

> **Running it a second time?** Idempotency keys are permanent. That is the point of them, so step 1
> will replay instead of moving money again, and the balances above no longer apply. Stop the server, run
> `rm -f wallet.db wallet.db-wal wallet.db-shm`, and start again, or use new keys (`my-test-11`, …).

### Step 4: The same on PostgreSQL

**4a. Start a PostgreSQL.** Pick one.

```sh
# Option A: Docker
docker compose up -d --wait postgres
export PG='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'

# Option B: Homebrew
brew services start postgresql@16
createuser -s wallet && createdb -O wallet wallet
export PG='postgres://wallet@localhost:5432/wallet?sslmode=disable'
```

Check the connection: `psql "$PG" -c 'select version();'` should print the server version.

**4b. Start the server on it** (stop the SQLite one first with Ctrl+C):

```sh
DB_DRIVER=postgres DB_DSN="$PG" make run
```

**Expect** `"db_driver":"postgres"` in the first log line, and `curl localhost:8080/readyz` →
`{"status":"ready"}`. That response proves the server reached Postgres.

**4c. Run the step 3 curls again.** You get the same statuses and the same final balances.

**4d. Look at the data in Postgres:**

```sh
psql "$PG" -c "select idempotency_key, status, failure_reason, amount from transfers where idempotency_key like 'my-%' order by created_at"
```

```
 idempotency_key |  status   |   failure_reason   | amount
-----------------+-----------+--------------------+--------
 my-test-1       | PROCESSED |                    |    500
 my-test-2       | FAILED    | INSUFFICIENT_FUNDS |    100
 my-test-3       | FAILED    | CURRENCY_MISMATCH  |    100
 my-burst-1      | PROCESSED |                    |     50
```

There is no row for the 404 or the 400, and exactly one row for the 10-request burst.

```sh
psql "$PG" -c 'select sum(balance) from wallets'       # 160000: money is conserved
```

To start over on Postgres:
`psql "${PG%/wallet*}/postgres" -c 'drop database wallet' -c 'create database wallet'`.

### Step 5: Automated checks on PostgreSQL

```sh
# the whole test suite on BOTH databases
TEST_POSTGRES_DSN="$PG" go test -race -count=1 ./...        # or, with Docker: make test-pg

# one-command end-to-end check:
#   tests -> real server on Postgres -> demo flows -> SQL invariants -> graceful shutdown
make verify-pg PG_ADMIN_DSN="${PG%/wallet*}/postgres?sslmode=disable"
```

**Expect** `make verify-pg` to print PASS for all 6 steps and end with
`Postgres end-to-end verification passed.` It creates its own `wallet_verify*` databases and leaves
yours alone. It needs a user that can `CREATE DATABASE`. Setup options, connection details and
troubleshooting are in **[docs/postgres.md](docs/postgres.md)**.

### Step 6: Code quality

```sh
make lint        # golangci-lint (config in .golangci.yml): no output means clean
make fmt-check   # gofmt: prints "needs gofmt" and the files if any
go vet ./...
```

### If something goes wrong

| Symptom | Fix |
|---|---|
| `address already in use` | another process holds :8080; stop it, or use `PORT=8081 make run` and curl `:8081` |
| balances differ from the table | the database was not fresh; reset it (see the note in step 3) |
| Postgres `connection refused` | Postgres is not running or is on another port; check with `psql "$PG" -c 'select 1'` |
| Postgres tests show `SKIP` | `TEST_POSTGRES_DSN` is not set in that shell |
| `/readyz` returns 503 | the server cannot reach its database |

More in [docs/postgres.md § Troubleshooting](docs/postgres.md#5-troubleshooting).

---

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8080` | |
| `DB_DRIVER` | `sqlite` | `sqlite` or `postgres`; anything else fails at startup |
| `DB_DSN` | `file:wallet.db` | required for postgres |
| `REQUEST_TIMEOUT` | `5s` | per-request deadline; expiry → 503 |
| `LOCK_TIMEOUT` | `5s` | Postgres `lock_timeout` / SQLite `busy_timeout` |
| `MAX_BODY_BYTES` | `1048576` | larger bodies → 413 |

See [`.env.example`](.env.example). Migrations are embedded in the binary and applied at startup.

## API reference

### `POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

Every transfer outcome returns the same body shape:

```json
{ "id": "01a0dd8d-…", "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2",
  "amount": 100, "status": "PROCESSED", "failureReason": null, "createdAt": "2026-09-26T11:50:53.774012Z" }
```

| Case | Status | Body / header |
|---|---|---|
| Processed | 201 | transfer, `status: PROCESSED` |
| Business rule failed (insufficient funds, currency mismatch, balance overflow) | 422 | transfer, `status: FAILED`, `failureReason` |
| Replay of a stored result (same key + same payload) | original status | same body, `Idempotent-Replayed: true` |
| Invalid input | 400 | `VALIDATION_ERROR` |
| Body too large | 413 | `PAYLOAD_TOO_LARGE` |
| Wallet not found | 404 | `WALLET_NOT_FOUND` (nothing stored; key stays reusable) |
| Same key, different payload | 409 | `IDEMPOTENCY_KEY_REUSED` |
| Timeout / lock timeout / retries exhausted | 503 | `TEMPORARILY_UNAVAILABLE` (safe to retry with the same key) |
| Unexpected | 500 | `INTERNAL_ERROR` (no internal details) |

Errors look like `{"error":{"code":"WALLET_NOT_FOUND","message":"wallet not found","requestId":"…"}}`.
Every response carries `X-Request-ID`: the incoming one is echoed if it is well-formed, otherwise a new one is generated.

Validation: `idempotencyKey` is 1–128 characters and wallet ids are 1–64 characters, both limited to
`[A-Za-z0-9._:-]`. The two wallets must differ. `amount` must be a JSON integer in `1..100000000`;
`10.5`, `"100"`, `1e2` and `null` are rejected. Unknown fields, arrays, empty bodies and non-JSON
content types are also rejected.

### Read APIs

| Endpoint | Returns |
|---|---|
| `GET /wallets/:id` | `{id, balance, currency, updatedAt}`, or 404 |
| `GET /wallets/:id/transfers?limit=20` | `{"transfers":[…]}` where the wallet is sender or receiver, newest first; `limit` 1–100, else 400; unknown wallet → 404 |
| `GET /healthz` | 200 if the process is up (never touches the DB) |
| `GET /readyz` | 200 if the DB is reachable, else 503 |

## What the tests cover

| Layer | What is covered |
|---|---|
| Domain (unit) | debit/credit edges, overflow, all-or-nothing `ApplyTransfer`, the full state transition matrix |
| Service command (unit) | every validation rule and boundary; request hash golden vector and collision resistance |
| Handler (httptest + fakes) | 35 malformed-input cases, 413, error mapping, replay header, request ids, panic recovery, timeouts, log fields |
| Integration (real DB, both drivers) | transfers, idempotency, the DB's own constraints, repositories, TxManager retries and lock timeouts, cancellation before commit, closed DB, end-to-end HTTP |
| Concurrency (real DB, both drivers) | 50-way double-spend attempt, 20× same key, same key with different amounts, 100 opposite-direction transfers (no deadlock), 500 random transfers, reads during writes |
| Binary end to end (Postgres) | `cmd/server` boots the real server with `DB_DRIVER=postgres` and drives every outcome over HTTP |

Integration tests run on SQLite always, and also on Postgres when `TEST_POSTGRES_DSN` is set. Each
test gets its own database (a SQLite file, or a Postgres schema).

After **every** integration test an invariant checker verifies:
- money is conserved;
- each PROCESSED transfer has exactly one matching DEBIT and one matching CREDIT;
- FAILED transfers have no ledger entries, and no PENDING transfer is left behind;
- each wallet's balance equals opening + credits − debits;
- every idempotency key is linked to its transfer.

## Design in brief

- **One transaction per request.** It covers the idempotency claim, the transfer row, both balances,
  both ledger entries and the final status. A crash or timeout at any point leaves nothing behind, and
  a retry is always safe.
- **Idempotency:** `INSERT … ON CONFLICT DO NOTHING` on the key's primary key, inside that transaction.
  An existing key with the same payload hash replays the stored transfer. A different hash gets 409.
  Concurrent duplicates wait on the key and then replay.
- **Concurrency:**
  - Postgres: `SELECT … FOR UPDATE` on both wallets in **sorted id order** (no deadlocks), `READ COMMITTED`,
    and a per-transaction `lock_timeout`.
  - SQLite: `BEGIN IMMEDIATE` plus a single-connection writer pool serialize writes (readers stay concurrent under WAL);
    waiting is bounded by the request deadline and `busy_timeout`.
  - Balance writes are a compare-and-set against the value read under the lock, so a missing lock
    would fail loudly instead of losing an update. A `CHECK (balance >= 0)` is the final safety net.
- **Indexes:** transfer history reads each direction's `(wallet, created_at)` index backwards and merges the
  two with `UNION ALL`. On Postgres this measured about 180× faster than an `OR` query for a wallet with 60k transfers.
- **Retries:** deadlocks and serialization failures retry the *whole* transaction (3 attempts, jittered).
  Lock timeouts return 503 without a retry.
- **Layering:** `handler` (Gin, DTOs) → `service` (orchestration, ports) → `repository/sqlrepo`
  (SQL written once, dialect for the differences) → `domain` (pure rules). There is no SQL outside
  the repository layer, and no Gin outside `handler`/`middleware`.

## Documentation

| Document | Contents |
|---|---|
| [docs/design.md](docs/design.md) | problem, contract, schema, idempotency, concurrency per driver, failure modes, testing strategy, tradeoffs, future work |
| [docs/manual-testing.md](docs/manual-testing.md) | the step 3 curl walkthrough, with the full expected response for every step |
| [docs/postgres.md](docs/postgres.md) | Postgres setup options, how the connection works, `make verify-pg`, troubleshooting |
| [requests.http](requests.http) | the same scenarios as clickable requests |

## Layout

```
cmd/server/                 main: config, wiring, graceful shutdown
internal/config/            env config
internal/domain/            Wallet, Transfer state machine, LedgerEntry, sentinel errors
internal/service/           TransferService, WalletService, command validation, ports
internal/repository/        factory; sqlrepo (shared SQL, TxManager, migrations); sqlite/ and postgres/ dialects + schemas
internal/handler/           router, handlers, DTOs, error mapping
internal/middleware/        request id, logging, recovery, timeout, body limit
tests/integration/          real-database tests + invariant checker
scripts/demo.sh             scripted walkthrough (make demo)
scripts/verify-postgres.sh  one-command Postgres end-to-end check (make verify-pg)
docs/                       design, manual testing, Postgres guide
```
