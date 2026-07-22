# Wallet Transfer Service

A service for moving money between wallets. It handles duplicate requests safely,
records every transfer as two ledger entries (one debit, one credit), and stays correct
when many requests run at the same time. The API accepts a transfer and returns quickly; a
background worker does the actual money movement. The database enforces correctness through
constraints, row locks, and unique keys, so a bug in the code cannot corrupt balances.

- **Language:** Go 1.25
- **Storage:** PostgreSQL 16
- **HTTP:** gorilla/mux
- **Config:** viper (defaults, then `config.yaml`, then environment variables)
- **Observability:** Zap logs, Prometheus metrics, Grafana dashboard, Jaeger tracing

---

## Architecture

```
HTTP (gorilla/mux)
  Handler       decode, validate, turn errors into HTTP status   (internal/handler)
    Service     validate, save the request, process a transfer   (internal/service)
      Repository   all SQL: locking, ledger, balances            (internal/repository)
        PostgreSQL

Worker pool + reaper   picks up pending transfers and finishes    (internal/worker)
                       them; recovers ones left unfinished
```

Transfers run in the background. The API saves a `PENDING` transfer and returns
`202 Accepted`. A worker then picks it up and completes it. The client asks for the result
with a follow-up GET.

```
POST /transfers   ->  save transfer as PENDING (idempotency_key is unique)  ->  202 {id, PENDING}
worker  claim  ->  UPDATE ... FOR UPDATE SKIP LOCKED, set PROCESSING
        run    ->  in one transaction: lock wallets, validate, write debit and credit,
                   update balances, set PROCESSED
                   business failure -> FAILED (saved); temporary error -> undo, try again
reaper  every N seconds  ->  transfers stuck in PROCESSING too long go back to PENDING
GET /transfers/{id}  ->  read the result (PROCESSED or FAILED)
```

### State machine

```
PENDING  ->  PROCESSING  ->  PROCESSED or FAILED
                 |
                 back to PENDING if a worker stops mid-way (the reaper puts it back)
```

---

## Design decisions

### Schema (`migrations/01_wallet_schema.up.sql`)

- **`wallet.balance`** holds the current balance. Reading and updating it is fast because it
  is a single row locked while a transfer runs. The `CHECK (balance >= 0)` rule is the final
  safety net: even a code bug cannot make a balance go negative.
- **`transfer`** stores the request and doubles as the duplicate-check record. It keeps its
  own `from`, `to`, and `amount`, so a repeated request can return the original result, and
  a `FAILED` transfer (which writes no ledger rows) can still be read back. The unique
  `idempotency_key` is what stops duplicates.
- **`ledger_entries`** is the permanent record of money movement. `UNIQUE (transfer_id,
  type)` means a transfer can have at most one debit and one credit, so if a worker
  processes the same transfer twice, the second attempt fails instead of double-counting.
- Money is stored as `NUMERIC(20,4)` (exact decimal), never floating point. Times use
  `TIMESTAMPTZ`.
- Two small partial indexes (on rows where state is `PENDING` or `PROCESSING`) keep the
  worker and reaper queries fast.

### Idempotency (safe duplicate handling)

The service runs `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING RETURNING`. If the key
already exists, it reads the existing transfer and returns that. The unique key in the
database, not the application code, is what guarantees a request runs at most once. Sending
the same request twice returns the same transfer: `202` the first time, `200` after that.
Redis is deliberately not used here. It would add a second place to store the same fact and
a chance for the two to disagree, without adding any real guarantee.

### Concurrency (many requests at once)

- The transfer locks **both wallets** with `SELECT ... FOR UPDATE`, always in the same order
  (by id), so two transfers going opposite directions cannot deadlock.
- Isolation is **READ COMMITTED**. The row locks provide the ordering, so there is no need
  for a stricter level that would force lots of retries.
- Workers claim work with `FOR UPDATE SKIP LOCKED`. Each worker grabs a different batch
  without waiting on the others, so you can run many workers (even on different machines).
- Double spending is stopped by the wallet lock plus `CHECK (balance >= 0)`. Reprocessing is
  made safe by the unique ledger key.

### Failure handling (business vs temporary)

| Failure | Example | Action | Why |
|---|---|---|---|
| Business (permanent) | not enough funds, inactive wallet | save as `FAILED` | asking again gives the same answer |
| Temporary (system) | lock or connection error (`40001` / `40P01`) | undo and try again | the request can still succeed later |

Temporary database conflicts are retried a few times with a growing, randomized wait. If a
transfer keeps failing and is picked up too many times, it is marked `FAILED` so it stops
looping forever.

#### Retry vs. business failure

Retry applies **only** to temporary errors (`40001` / `40P01`), never to business failures.
Not enough funds or an inactive wallet will never succeed on a retry, so those are marked
`FAILED` right away.

This matters under load. When two transfers spend from the same wallet, the second one does
not spin in a retry loop. It simply **waits** for the first one's lock (waiting is normal,
not an error, because we use READ COMMITTED). When it gets the lock it reads the updated
balance:

| Source balance | 1st transfer | 2nd transfer (after the wait) |
|---|---|---|
| enough for one | PROCESSED | reads the lower balance, fails on not-enough-funds, marked FAILED (no retry) |
| enough for both | PROCESSED | reads the lower balance, still enough, marked PROCESSED (it just waited) |

So retry is a rarely-used safety net; the real mechanism is waiting for the lock. A `FAILED`
transfer is final. To try again later (say, once funds arrive) the client sends a new
transfer with a new idempotency key.

---

## Running

### With Docker Compose (everything)

```bash
docker compose up -d --build
```

This starts Postgres, applies the migrations and seed data, then starts the app,
Prometheus, Grafana, and Jaeger:

| Service    | URL                              |
|------------|----------------------------------|
| API        | http://localhost:8080            |
| Metrics    | http://localhost:8080/metrics    |
| Prometheus | http://localhost:9090            |
| Grafana    | http://localhost:3000 (admin/admin) |
| Jaeger     | http://localhost:16686           |

### Locally

```bash
make up          # start Postgres
make migrate-up  # apply the schema (needs the golang-migrate CLI)
make seed        # load demo wallets
make run         # start the service
```

Demo wallet ids: `1111...1111` (balance 1000), `2222...2222` (0), `3333...3333` (inactive).

---

## API

### `POST /api/v1/transfers`

```bash
curl -i -X POST http://localhost:8080/api/v1/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "abc123",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId":   "22222222-2222-2222-2222-222222222222",
    "amount": 100
  }'
```

- `202 Accepted` with a `Location` header: a new transfer (state `PENDING`)
- `200 OK`: the same request was already seen; returns the original transfer
- `400`: invalid request. `422`: wallet does not exist

### `GET /api/v1/transfers/{id}`

```bash
curl http://localhost:8080/api/v1/transfers/<id>
```

Returns the current state: `PENDING` (also while a worker is processing it), then
`PROCESSED` or `FAILED` (with a `failureReason` when it failed). `PROCESSING` is an
internal state and is shown as `PENDING` to callers.

### Operational endpoints

`GET /healthz` (is the process up), `GET /readyz` (can it reach the database),
`GET /metrics` (Prometheus).

---

## Testing

```bash
make test        # all tests (unit + integration)
make test-unit   # only the tests that need no database
make cover       # coverage over the main packages (about 90%)
```

Unit tests use in-memory fakes (`internal/tests/mock`) to check every path, including the
failure paths (not enough funds, inactive wallet, duplicate ledger entry, temporary errors,
giving up after too many tries, worker and reaper errors), all without a database.
Integration tests only run when `TEST_DATABASE_URL` is set and skip otherwise, so CI passes
without a database. Coverage of the main packages is about 90% (domain, logger, and metrics
are at 100%); the rest is database-error handling and server start/stop code. Covered areas:

- **domain**: validation, balance checks, allowed state changes (unit)
- **handler**: status codes, error mapping, strict JSON parsing (unit, no database)
- **repository**: safe duplicate insert, `FOR UPDATE`, duplicate-ledger check, reaper (integration)
- **service**: business vs temporary failures, safe reprocessing, concurrent double-spend (integration)
- **worker**: processing the queue and recovering an unfinished transfer
- **full stack**: real `POST`, worker finishes it, GET shows `PROCESSED`

---

## Observability

- **Logs**: structured JSON (Zap), one line per request with a `request_id`.
- **Metrics**: `transfers_enqueued_total`, `transfers_processed_total{result}`,
  `transfer_processing_duration_seconds`, `http_requests_total{method,route,status}`,
  `http_request_duration_seconds`, and `db_pool_*_conns`.
- **Grafana**: a ready-made dashboard (`deploy/grafana/dashboards`) showing processing rate,
  request rate, latency, and database connection usage.
- **Tracing**: OpenTelemetry spans on HTTP requests and on transfer processing, sent over
  OTLP. `docker compose up` wires up Jaeger (UI at http://localhost:16686). Off by default
  when running locally (`tracing_enabled`).

---

## Configuration

Every value comes from config (defaults, then `config.yaml`, then environment variables,
where the environment wins). See `config.example.yaml`. Common settings: `HTTP_ADDR`,
`LOG_LEVEL`, `DATABASE_URL` (or the `DB_*` parts), `DB_MAX_CONNS`, `WORKER_COUNT`,
`WORKER_BATCH_SIZE`, `WORKER_POLL_INTERVAL`, `REAPER_INTERVAL`, `STUCK_AFTER`,
`MAX_ATTEMPTS`.

---

## Project layout

```
internal/
  config/                 configuration (viper)
  domain/                 entities, enums, state machine, validation (no dependencies)
  db/                     database connection pool (pgx)
  logger/                 logger (zap)
  observability/
    metrics/              Prometheus metrics
    tracing/              OpenTelemetry setup
  repository/             Store and TxStore interfaces plus the Postgres code
  service/                request saving and transfer processing plus retry
  handler/                HTTP handlers, request/response types, middleware, start/stop
  worker/                 background pool that claims and processes, plus the reaper
  tests/
    integration/          database-backed tests (repository, service, worker, API)
    mock/                 in-memory fakes for unit tests
    testutils/            shared helpers for integration tests
main.go                   wires everything by hand and handles graceful shutdown
migrations/               schema (golang-migrate) and seed.sql (demo data)
deploy/                   Prometheus and Grafana config
```

---

## Tradeoffs and possible extensions

- **Background processing (202 then poll).** Chosen so the service scales and so recovery of
  unfinished transfers is real and testable. A synchronous mode that waits for the result
  could be added on top without changing the core.
- **No wallet-creation API.** Out of scope; wallets are seeded. In production, a small job
  that checks the sum of ledger entries matches each stored balance would sit alongside this.
- **Graceful shutdown** is confirmed inside the Linux container. On Windows, git-bash `kill`
  force-stops the process, so the drain-then-exit path is checked in the container instead.
```
