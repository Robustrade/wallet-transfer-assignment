# Wallet Transfer Service

## Overview

A small wallet-to-wallet transfer service implementing [ASSIGNMENT.md](ASSIGNMENT.md).
`POST /transfers` atomically updates stored balances and records a double-entry
ledger. Requests with an idempotency key replay their original committed result.
`GET /health` returns `{"status":"ok"}` for process liveness, not database readiness.

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Money is an `int64` count of minor units. Amounts must be positive, wallet IDs must
be nonblank and distinct, and both wallets must exist. The HTTP handler rejects
unknown fields, trailing JSON values, malformed Unicode, and bodies over 1 MiB.

| HTTP status | Outcome |
| --- | --- |
| 201 | PROCESSED; transfer ID, source, destination, and amount |
| 422 | Committed FAILED transfer; transfer ID and business error |
| 400 | Malformed or invalid request |
| 404 | Wallet not found |
| 409 | Same idempotency key with different transfer parameters |
| 500 | Unexpected/database failure; no internal details exposed |

Errors use `{"error":{"code":"...","message":"..."}}`. Persisted business
failures also include `transferId` and `state: "FAILED"`.

## Stack

- Go 1.21
- PostgreSQL
- `database/sql` with `github.com/jackc/pgx/v5/stdlib`
- Standard `net/http` and `encoding/json`
- `log/slog` with an injected JSON logger
- Testcontainers PostgreSQL module for integration tests

## Architecture

```text
HTTP Handler -> Service -> Repository -> PostgreSQL
```

`cmd/server` loads configuration and wires dependencies. `internal/handler/http`
handles transport validation and response mapping. `internal/service` owns business
rules, idempotency, and the transaction callback. `internal/repository/postgres`
implements SQL against one `*sql.Tx`. `internal/domain` contains types, validation,
and explicit state transitions, independent of HTTP and PostgreSQL. Request
contexts propagate through every layer into database operations.

## Data model

| Table | Purpose and constraints |
| --- | --- |
| `wallets` | TEXT ID, stored BIGINT balance, timestamps; balance cannot be negative |
| `transfers` | Source/destination wallet FKs, positive BIGINT amount, optional unique key, timestamps, PENDING/PROCESSED/FAILED status; wallets must differ |
| `ledger_entries` | Wallet/transfer FKs, positive amount, DEBIT/CREDIT type; `UNIQUE (transfer_id, entry_type)` prevents duplicate legs |
| `idempotency_records` | Key primary key, request fingerprint, unique transfer reference, original response status/body, timestamps |

Deferred FKs tie each keyed transfer to its matching reservation at commit. History
is protected from cascading deletion. Primary-key and unique indexes cover wallet,
transfer, idempotency-key, and per-transfer ledger lookups. See
[migrations/README.md](migrations/README.md) for constraint details.

## Transaction boundary

Input validation runs before the transaction. The actual success path is:

```text
BEGIN (READ COMMITTED)
reserve/check idempotency key, if supplied
lock both wallets and verify both exist
create PENDING transfer
check source funds and destination int64 overflow
update source and destination balances
insert source DEBIT and destination CREDIT for the same amount/transfer
mark transfer PROCESSED
store final idempotency response, if keyed
COMMIT
```

PENDING is inserted **before** balance validation so expected business rejections
can be recorded. The service defines this callback; the repository begins exactly
one transaction, commits only on callback success, and defers rollback for early
returns and panics. No ordinary repository method starts its own transaction.
The service returns a terminal result only after commit succeeds.

### FAILED path

Insufficient funds or destination overflow transitions the inserted PENDING row
to FAILED, stores the original failed response, and commits. FAILED transfers have
no balance updates or DEBIT/CREDIT entries because no money moved. PENDING is
never intentionally committed as unfinished work.

Invalid input creates no transfer. Missing wallets cause rollback of any key
reservation without creating a transfer, because wallet FKs must remain valid.
Infrastructure/database errors roll back the entire transaction, including any
partial balance or ledger writes. They do not safely persist FAILED. A connection
failure during commit may leave the outcome unknown; retry with the same key to
resolve it. There are no automatic transaction retry loops.

## Concurrency

The repository locks wallets using:

```sql
SELECT id, balance, created_at, updated_at
FROM wallets
WHERE id IN ($1, $2)
ORDER BY id
FOR UPDATE;
```

Both locks remain held through financial writes and commit/rollback. A waiting
transfer receives the updated balance after its predecessor commits. IDs are
immutable in the service, so opposite-direction transfers acquire locks in the
same database order. Rows are mapped back to source/destination by ID, not position.
Wallet locks precede transfer insertion and its FK checks, avoiding a prior shared
lock that would later need upgrading.

Explicit READ COMMITTED keeps statement-snapshot behavior predictable even if the
database default changes. Ordered locks reduce circular waits; other writers,
additional resources, FK lock upgrades, or different locking orders can still
cause deadlocks. SQLSTATE `40P01` and `40001` are surfaced as transaction failures
requiring a new whole transaction. No process-local mutex is used: PostgreSQL
coordinates independent service instances and connections.

## Idempotency

Keys are optional, globally unique for this endpoint, and have no expiration.
Without a key, each request is independent and lost-response retries can transfer
again. The fingerprint is lowercase SHA-256 of Go's compact JSON encoding of
`[fromWalletId,toWalletId,amount]`, retaining exact integer arithmetic.

The first request claims the key with `INSERT ... ON CONFLICT (idempotency_key)
DO NOTHING RETURNING idempotency_key`. A returned row owns the claim. On no row,
a separate statement reads the existing record using a fresh READ COMMITTED
snapshot. Same fingerprint returns the exact original status/body and transfer
ID, including committed FAILED results; different fingerprint returns 409.

A concurrent duplicate waits for the owner's transaction. After commit it replays
the result; after rollback it can acquire the claim. Reservation and final result
commit together, so no externally visible IN_PROGRESS state is needed. An
unexpected committed record without a result produces an error, never reexecution.
Normal duplicate handling does not deliberately raise `23505` and continue in an
aborted transaction.

## Testing

- Domain/service tests cover validation, state transitions, success, business
  failures, replay/conflicts, and injected failure orchestration.
- HTTP tests cover request decoding, JSON errors, response mapping, context
  forwarding, and preservation of original success/FAILED results.
- PostgreSQL integration tests use real transactions for locking, rollback,
  uniqueness, FKs, and ledger invariants. They include competing debits, same-key
  bursts, opposite-direction transfers, and lost-response retries.
- Concurrency tests use goroutines/channels and observed PostgreSQL lock waits,
  then assert committed database state. No arbitrary sleep establishes correctness.

```sh
go fmt ./...
go vet ./...
go test ./...
go test -race ./...

# Requires a running Docker-compatible runtime and image-pull access:
go test -tags=integration ./... -count=1 -shuffle=on
go test -race -tags=integration ./... -count=1
```

Testcontainers v0.30.0 starts one package-shared `postgres:16.3-alpine` container,
waits for readiness, then gives each case a fresh migrated schema and its own seed
data/pool. Test cleanup drops schemas and closes pools; TestMain terminates the
container. `TEST_DATABASE_URL` is not used. Missing Docker fails the integration
suite; default tests exclude it via the build tag.

The default suite and race checks passed during development. Integration tests
compile, but live PostgreSQL execution remains unverified on the development host
because Docker was unavailable.

On macOS, if Go 1.21 binaries fail with `missing LC_UUID`, run commands with
`CGO_ENABLED=1 GOFLAGS='-ldflags=-linkmode=external'` (requires Xcode command-line
tools). For example:

```sh
CGO_ENABLED=1 GOFLAGS='-ldflags=-linkmode=external' go test -race ./...
```

## Local setup

Requires Go 1.21, `psql`, and PostgreSQL. These commands provision a fresh local
Docker database; an existing PostgreSQL database can also be used by changing
`DATABASE_URL`.

```sh
docker run --name wallet-postgres \
  -e POSTGRES_USER=wallet -e POSTGRES_PASSWORD=wallet \
  -e POSTGRES_DB=wallet -p 127.0.0.1:5432:5432 \
  -d postgres:16.3-alpine
until docker exec wallet-postgres pg_isready -U wallet -d wallet; do sleep 1; done

export DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
go mod download
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_initial.up.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -c "INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000), ('wallet_2', 0);"
go run ./cmd/server
```

In another terminal:

```sh
curl -i http://localhost:8080/health
curl -i http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Only `DATABASE_URL` is required. The application reads environment variables
directly; it does not load `.env` files.

| Variable | Default |
| --- | --- |
| `DATABASE_URL` | Required PostgreSQL connection string |
| `HTTP_ADDR` | `:8080` |
| `DB_MAX_OPEN_CONNS` | `10` |
| `DB_MAX_IDLE_CONNS` | `5` |
| `DB_CONN_MAX_LIFETIME` | `30m` |
| `DB_CONN_MAX_IDLE_TIME` | `5m` |
| `HTTP_REQUEST_TIMEOUT` | `10s` |
| `STARTUP_TIMEOUT` | `5s` |
| `SHUTDOWN_TIMEOUT` | `10s` |

Durations must be positive Go duration strings. Open connections must be positive;
idle connections must be between zero and the open limit. HTTP header/read/write/
idle timeouts are fixed at 5s/10s/15s/60s. Keep the request timeout below the write
timeout to leave time for responses. Startup pings PostgreSQL before serving HTTP.
SIGINT/SIGTERM drains active requests; shutdown timeout cancels request contexts
and closes HTTP connections before pool cleanup.

Migrations are manual and contain their own transactions. To undo the schema
(**deletes all four tables and their data**):

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_initial.down.sql
```

## Tradeoffs

PostgreSQL is the assignment's preferred database and provides atomic transactions,
row locks, unique arbitration, and referential constraints needed for this flow.
Stored balances make locked checks inexpensive; updates and ledger writes share
one transaction. Integer minor units avoid floating-point rounding.

The ledger unique constraint enforces at most one entry of each type; the service
ensures both exist with correct wallets/amounts before marking PROCESSED. State
transitions and final-result completeness are also workflow responsibilities,
not triggers. Arbitrary direct SQL writers must preserve these invariants.

The service assumes one currency/unit and pre-provisioned wallets. There is no
wallet-management API, authentication, migration runner, background worker, retry
loop, or idempotency cleanup. Logs carry transfer/key/wallet IDs and outcomes,
without raw database configuration or driver errors. See [DESIGN.md](DESIGN.md)
for detailed decisions and remaining scope assumptions.
