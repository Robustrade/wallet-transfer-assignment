# Wallet Transfer Service — Design

## 1. Overview

How the service defined in [requirements.md](requirements.md) and
[api-spec.md](api-spec.md) is built.

A Go service on PostgreSQL. Each `POST /transfers` runs in one database
transaction. It claims the idempotency key, locks both wallets, and then
either records the transfer as `FAILED` or moves the balances, writes two
ledger entries, and marks it `PROCESSED`.

| Question | Answer | Section |
|----------|--------|---------|
| Which database? | PostgreSQL; tests start their own with embedded-postgres | §2 |
| Stored or derived balance? | Stored `wallets.balance`, with `CHECK (balance >= 0)` | §4 |
| How do opening balances keep the ledger balanced? | `wallets.opening_balance`; the ledger records only transfers | §4 |
| How is the idempotency key stored? | A `UNIQUE` column on `transfers`, claimed with `INSERT … ON CONFLICT DO NOTHING` | §4, §6 |
| How are concurrent debits made safe? | `FOR NO KEY UPDATE` locks on both wallets, in ID order, at READ COMMITTED | §7 |
| What enforces consistency? | Constraints for single-row rules; the transaction plus reconciliation tests for the rest | §8 |

## 2. Technology Choices

| Concern       | Choice | Why |
|---------------|--------|-----|
| Language      | Go 1.24 | Required; matches CI (requirements §5) |
| Database      | PostgreSQL 16 | Preferred by the assignment; row-level locks and constraints enforce correctness in the database |
| Driver        | `github.com/jackc/pgx/v5` (`pgxpool`), hand-written SQL | Explicit transactions; every lock and constraint is visible in the SQL |
| HTTP          | `net/http` `ServeMux` | Go 1.22+ method patterns (`POST /transfers`) cover one endpoint with no dependency |
| Schema & seed | `schema.sql` and `seed.sql`, embedded with `go:embed`, applied on startup and in test setup | One schema version needs no migration tool |
| IDs           | UUIDv7 from `github.com/google/uuid` | Spec §3 |
| Logging       | `log/slog` | Requirements NFR-5 |
| Test database | `github.com/fergusstrange/embedded-postgres` | Tests start their own Postgres, so `go test ./...` needs no external service (D-16) |
| Local run     | Docker Compose starts Postgres; the service runs with `go run` | A reviewer needs only Docker and Go |
| Configuration | Environment variables `DATABASE_URL` and `HTTP_ADDR` (default `:8080`) | No config files to manage |

### Alternatives Considered

**SQLite.** The assignment accepts it, and tests would need nothing to start.
Rejected because SQLite allows one writer at a time: transfers would queue
behind a database-wide lock. That is correct, but it leaves no row-level
locking strategy to design or show.

**PostgreSQL with opt-in database tests.** `go test ./...` would run only unit
tests with fakes, and the idempotency and concurrency tests would need a build
tag and a running Postgres. Rejected because CI would never run the tests that
matter most.

### Risks

- embedded-postgres downloads Postgres binaries on the first test run, so that
  run needs network access. CI has it: the workflow downloads golangci-lint
  and runs `apt-get`.
- Postgres refuses to run as root, so the tests fail if CI runs as root. The
  workflow uses `sudo`, which suggests it does not.
- Starting Postgres adds a few seconds to the test run.

## 3. Architecture & Project Layout

Module: `github.com/pravinkanna/wallet-transfer-assignment`

```text
.
├── cmd/server/main.go     # wiring: config, pool, schema, server
├── internal/
│   ├── domain/            # Transfer, LedgerEntry, states, rules, errors
│   ├── service/           # transfer workflow, idempotency
│   ├── repository/        # Postgres (pgx) persistence
│   │   └── sql/           # schema.sql, seed.sql (go:embed)
│   ├── handler/           # HTTP decode, error mapping, encode
│   └── apitest/           # API tests against embedded Postgres
├── docs/
├── compose.yaml
└── go.mod
```

### Layers

Each package is one layer from ASSIGNMENT.md's "Architecture Expectations"
(requirements NFR-3).

| Package               | Layer         | Responsibilities (ASSIGNMENT.md)                                       | In this service |
|-----------------------|---------------|------------------------------------------------------------------------|-----------------|
| `internal/handler`    | Handler       | request validation, transport mapping, invoking service logic          | Rejects invalid JSON and unknown fields (spec §6 steps 1–2), builds a domain request, calls the service, and maps results and errors to status codes and bodies (spec §3, §4) |
| `internal/service`    | Service       | business logic, orchestration, idempotency behavior, transfer workflow | Runs the transfer in one transaction: idempotency check, wallet lookup, balance decision, recording the outcome (spec §6 steps 6–8) |
| `internal/repository` | Repository    | persistence operations, database interaction                           | One method per SQL statement, plus `InTx` to run a function in a transaction; makes no business decisions |
| `internal/domain`     | Domain models | entities, state transitions, validation rules                          | `Wallet`, `Transfer`, `LedgerEntry`; the `PENDING → PROCESSED / FAILED` transitions; field, amount, and same-wallet rules (spec §6 steps 3–5); domain errors |
| `cmd/server`          | —             | —                                                                      | Reads configuration, opens the pool, applies schema and seed, builds the layers, starts the HTTP server |

As ASSIGNMENT.md requires, handlers stay thin, and persistence logic is not
mixed with business rules.

### Dependency Rules

```text
handler    → service, domain
service    → domain        (defines the repository interfaces it uses)
repository → domain        (implements those interfaces without importing service)
domain     → nothing internal
cmd/server → everything    (wiring only)
```

- The handler never touches the database.
- The repository never decides a transfer's outcome.
- The domain has no dependency on HTTP or SQL.

### Transactions Across Layers

The repository exposes `InTx(ctx, fn)`. It begins a transaction, passes a
transaction-scoped repository to `fn`, and commits if `fn` returns nil or
rolls back otherwise. The service calls `InTx` and runs the whole workflow
inside `fn`, so the service decides the outcome while every SQL statement
stays in the repository.

## 4. Data Model

### Schema

`internal/repository/sql/schema.sql`:

```sql
DO $$
BEGIN
    CREATE TYPE transfer_state AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS wallets (
    id              VARCHAR(255) PRIMARY KEY CHECK (id <> ''),
    opening_balance BIGINT       NOT NULL CHECK (opening_balance >= 0),
    balance         BIGINT       NOT NULL CHECK (balance >= 0),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id              UUID           PRIMARY KEY,
    idempotency_key VARCHAR(255)   NOT NULL UNIQUE CHECK (idempotency_key <> ''),
    from_wallet_id  VARCHAR(255)   NOT NULL REFERENCES wallets (id),
    to_wallet_id    VARCHAR(255)   NOT NULL REFERENCES wallets (id),
    amount          BIGINT         NOT NULL CHECK (amount > 0),
    state           transfer_state NOT NULL,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          BIGINT            GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id UUID              NOT NULL REFERENCES transfers (id),
    wallet_id   VARCHAR(255)      NOT NULL REFERENCES wallets (id),
    type        ledger_entry_type NOT NULL,
    amount      BIGINT            NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ       NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, type)
);
```

Postgres has no `CREATE TYPE IF NOT EXISTS`. The schema is applied on every
startup, so each `CREATE TYPE` is wrapped in a `DO` block that ignores
"already exists".

### What Each Constraint Guarantees

| Constraint | Guarantees |
|------------|------------|
| `wallets.balance CHECK (>= 0)` | The database refuses any update that would overdraw a wallet, even if code is wrong |
| `transfers.idempotency_key UNIQUE` | One transfer per key, globally; also makes a concurrent second insert wait for the first (§6) |
| `transfers CHECK (from <> to)`, `amount > 0` | Same rules as spec §6 steps 4–5, enforced again in the database |
| `transfer_state` and `ledger_entry_type` enums | Only the three states and the two entry types exist |
| `ledger_entries.transfer_id` FK, `NOT NULL` | A ledger entry cannot exist without a transfer |
| `ledger_entries UNIQUE (transfer_id, type)` | At most one DEBIT and one CREDIT per transfer |
| FKs to `wallets` | Transfers and entries only reference existing wallets |

Rules that span rows, such as "a `PROCESSED` transfer has exactly two entries"
and "a `FAILED` transfer has none", cannot be expressed as plain constraints.
§8 covers how they are enforced.

### Indexes

Every query the service runs is served by an index that a primary key or
`UNIQUE` constraint already creates:

| Query | Index |
|-------|-------|
| Find a transfer by idempotency key (spec §6 step 6) | `transfers (idempotency_key)` |
| Lock wallets by ID (spec §6 steps 7–8) | `wallets (id)` |
| Find a transfer's ledger entries | `ledger_entries (transfer_id, type)` |

No other indexes are added. Nothing queries transfers or entries by wallet,
because the history API is out of scope (requirements §6). An index on
`ledger_entries (wallet_id)` would come with that API.

### Seed Data

`internal/repository/sql/seed.sql` inserts three wallets with
`ON CONFLICT (id) DO NOTHING`, so applying it on every startup is safe:

| id         | opening_balance | balance |
|------------|-----------------|---------|
| `wallet_1` | 1000            | 1000    |
| `wallet_2` | 1000            | 1000    |
| `wallet_3` | 0               | 0       |

Money only moves between wallets, so no balance can exceed the seeded total
(2000). That is far below int64 max, so no balance can overflow.

### Alternatives Considered

**Balance derived from the ledger.** Rejected: every transfer would have to
sum the wallet's entries, and a wallet row would still need locking to stop
two transfers spending the same funds. A stored balance with
`CHECK (balance >= 0)` lets the database refuse an overdraft directly.

**Genesis transfers for opening balances.** Rejected: funding wallets through
transfers needs a system wallet that can go below zero, which contradicts
FR-2. The `opening_balance` column keeps every balance explainable:
`balance = opening_balance + credits − debits`.

**A separate `idempotency_records` table** (suggested by the assignment).
Rejected: only stored transfers use up a key (spec §5), so every key belongs
to exactly one transfer. A `UNIQUE` column on `transfers` gives the same
guarantee with one table fewer and no join. A replay rebuilds its response
from the transfer's `state`, and "same body" compares the transfer's own
`from_wallet_id`, `to_wallet_id`, and `amount`.

## 5. Transfer Flow

One `POST /transfers` request, following the order of checks in spec §6.

### Before the Transaction (spec §6 steps 1–5)

**Handler**

1. Read the body through `http.MaxBytesReader` with a 64 KiB limit; a larger
   body → `REQUEST_TOO_LARGE`. Decode it into `map[string]json.RawMessage`. A
   body that is not a single JSON object (invalid, empty, not an object, or
   followed by trailing data) → `INVALID_JSON`.
2. Any key other than the four in spec §2 → `UNKNOWN_FIELD`.
3. Turn the raw values into domain input. Each string field must be a JSON
   string (otherwise `INVALID_FIELD`) and is unquoted. `amount` is passed on
   as its raw JSON text. A missing field or `null` becomes `""`.

**Domain** (`domain.NewTransferRequest`)

4. `idempotencyKey`, `fromWalletId`, and `toWalletId` are 1–255 characters
   with no NUL (U+0000), which Postgres text cannot store, and `amount` is
   present → otherwise `INVALID_FIELD`.
5. `amount` parses with `strconv.ParseInt(raw, 10, 64)` and is greater than
   0 → otherwise `INVALID_AMOUNT`. The raw texts `"100"`, `100.5`, `100.0`,
   and `1e2` do not parse, so all are rejected.
6. `fromWalletId` differs from `toWalletId` → otherwise `SAME_WALLET`.

`amount` travels as raw text because decoding it straight into an `int64`
would report a wrong amount during decoding, before the missing-field check.
Raw text keeps the spec order (step 3 before step 4) and keeps
`encoding/json` out of the domain.

### The Transaction (spec §6 steps 6–8)

The service runs these steps inside `InTx` (§3) at the default isolation
level, READ COMMITTED (§7).

**Claim the key** (step 6). The transfer is created as `PENDING`:

```sql
INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state)
VALUES ($1, $2, $3, $4, $5, 'PENDING')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id;
```

| Result                     | Meaning                          | Next |
|----------------------------|----------------------------------|------|
| A row is returned          | The key is new                   | Lock the wallets |
| No row is returned         | The key already exists           | Load the existing transfer |
| FK violation (`23503`)     | A wallet does not exist (step 7) | `WALLET_NOT_FOUND`; roll back |

If another transaction holding the same key is still running, this `INSERT`
waits for it to finish (§6).

**Key already exists:**

```sql
SELECT id, from_wallet_id, to_wallet_id, amount, state
FROM transfers
WHERE idempotency_key = $1;
```

Same `from_wallet_id`, `to_wallet_id`, and `amount` → replay that transfer.
Anything different → `IDEMPOTENCY_KEY_REUSED`. Either way the transaction
writes nothing.

**Lock the wallets** (steps 7–8), one statement per wallet, lower ID first
(§7):

```sql
SELECT balance FROM wallets WHERE id = $1 FOR NO KEY UPDATE;
```

**Decide** (step 8). The service compares the source balance with `amount`.

Insufficient funds:

```sql
UPDATE transfers SET state = 'FAILED' WHERE id = $1 AND state = 'PENDING';
```

Commit → `422`, `FAILED`.

Sufficient funds:

```sql
UPDATE wallets SET balance = balance - $2 WHERE id = $1;  -- source
UPDATE wallets SET balance = balance + $2 WHERE id = $1;  -- destination
INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
VALUES ($1, $2, 'DEBIT', $4), ($1, $3, 'CREDIT', $4);
UPDATE transfers SET state = 'PROCESSED' WHERE id = $1 AND state = 'PENDING';
```

Commit → `201`, `PROCESSED`.

**Guarded transitions.** The domain's `Transfer` allows `MarkProcessed` and
`MarkFailed` only from `PENDING`. The SQL repeats that guard with
`AND state = 'PENDING'`, and the repository treats zero updated rows as an
error.

### Responses

The service returns the transfer, new or replayed, and the handler maps it:

| Outcome                                           | Response |
|---------------------------------------------------|----------|
| `PROCESSED` transfer (new or replayed)            | `201` (spec §3) |
| `FAILED` transfer (new or replayed)               | `422` (spec §3) |
| Domain error                                      | Its code from spec §4 |
| Any other error                                   | `500 INTERNAL_ERROR`. An error before `COMMIT` rolls the transaction back; a failed `COMMIT` leaves the outcome unknown (§9) |

## 6. Idempotency

This section answers the four questions in ASSIGNMENT.md's "Idempotency
Requirements".

### How the Key Is Stored

In `transfers.idempotency_key` (`VARCHAR(255) NOT NULL UNIQUE`, §4), on the
same row as the transfer it created. Keys are global (requirements D-14) and
never deleted (D-12). They live in Postgres, so they survive restarts.

### How Duplicates Are Detected

By the key-claim `INSERT … ON CONFLICT (idempotency_key) DO NOTHING` (§5).
The unique index decides; there is no application-level check before the
insert.

| Situation                                | What Postgres does                | Result |
|------------------------------------------|-----------------------------------|--------|
| New key                                  | Inserts the row                   | The transfer proceeds |
| Key committed earlier                    | Skips the insert, returns no row  | Load and compare → replay or `409` |
| Key held by a transaction still running  | Waits for that transaction to end | If it commits, as above; if it rolls back, the insert proceeds as new |

After waiting, the follow-up `SELECT` sees the other transaction's committed
row, because READ COMMITTED takes a fresh snapshot for each statement.

### How the Original Result Is Returned

The response is rebuilt from the stored transfer rather than stored itself:

- status: `201` if `PROCESSED`, `422` if `FAILED`
- body: `transferId` and `state`

Both end states are final (D-10), so a rebuilt response always equals the
original. No status code needs storing, because the state determines it.

### How Duplicate Side Effects Are Prevented

The key claim and every side effect (balance changes, ledger entries, state
change) commit in one transaction. Either the key and all its effects are
stored, or none are:

- **Same request sent twice:** the second finds the key and replays.
- **First request commits, response lost:** the retry finds the key and
  replays.
- **First request fails before commit:** nothing is stored, including the
  key, so the retry runs as new.
- **First request's `COMMIT` fails (`500`, outcome unknown):** either
  everything or nothing was stored, so the retry replays or runs as new (§9).
- **Process restarts:** the key is in Postgres.

### Alternatives Considered

**Check, then insert** (`SELECT` the key, `INSERT` if absent). Rejected: two
concurrent requests can both see no key, and one insert then fails with a
unique violation that still has to be handled. `ON CONFLICT` checks and
claims in one statement.

**An in-memory or Redis key store.** Rejected: an in-memory store is lost on
restart, and neither commits atomically with the transfer, so a crash between
the two writes could keep the key and lose the transfer, or the reverse.

**Storing the literal response.** Not needed: the state determines the
response.

## 7. Concurrency Control

**Strategy:** pessimistic row locks at READ COMMITTED. The service locks both
wallets with `SELECT … FOR NO KEY UPDATE`, in ascending ID order, before
reading the source balance (§5).

- **Lock before reading:** the balance check and the debit happen under one
  lock, so there is no read-then-write race.
- **Both wallets, in ID order:** every transaction locks in the same order,
  so transfers in opposite directions cannot deadlock. IDs are sorted in Go
  and locked one statement each.
- **`FOR NO KEY UPDATE`, not `FOR UPDATE`:** the key-claim `INSERT` takes
  `FOR KEY SHARE` on both wallets for its foreign keys. `FOR UPDATE` conflicts
  with that and would deadlock two transfers on the same wallets;
  `FOR NO KEY UPDATE` does not, yet still queues transfers on a wallet.
- **Backstop:** `CHECK (balance >= 0)` (§4) rejects any overdraft that slips
  past the code.

**Example:** `wallet_1` holds 1000; A and B each send 800 from it at once.

| Step | A                                | B                                      |
|------|----------------------------------|----------------------------------------|
| 1    | Locks `wallet_1`, reads 1000     | Tries to lock `wallet_1`, waits        |
| 2    | Debits 800, `PROCESSED`, commits |                                        |
| 3    |                                  | Locks `wallet_1`, reads 200 → `FAILED` |

**Tradeoff:** transfers sharing a wallet run one at a time; others run in
parallel. An unexpected abort, such as a deadlock (`40P01`), returns `500`
with no automatic retry (requirements D-17).

**Alternatives considered:**

| Alternative                                  | Why not |
|----------------------------------------------|---------|
| Conditional `UPDATE … WHERE balance >= amt`  | Still needs ordered updates to avoid deadlocks; hides the balance decision in SQL |
| Optimistic locking or SERIALIZABLE           | Conflicts need retries; without auto-retry (D-17) they surface as `500`s |
| One global lock                              | Serializes even unrelated transfers |
| In-process mutex                             | Doesn't hold across processes; moves correctness out of the database |

## 8. Consistency Invariants

| # | Invariant | Enforced by |
|---|-----------|-------------|
| 1 | A wallet balance is never negative | Database: `CHECK (balance >= 0)`; code: balance check under lock (§7) |
| 2 | One transfer per idempotency key | Database: `UNIQUE (idempotency_key)` |
| 3 | A ledger entry always belongs to a transfer | Database: `transfer_id NOT NULL` + FK |
| 4 | A transfer has at most one DEBIT and one CREDIT | Database: `UNIQUE (transfer_id, type)` |
| 5 | A `PROCESSED` transfer has exactly two entries: DEBIT on the source and CREDIT on the destination, both for `amount` | Code: one transaction writes both (§5) |
| 6 | A `FAILED` transfer has no entries and changed no balance | Code: the `FAILED` path only updates the state (§5) |
| 7 | No transfer is committed as `PENDING` | Code: the state changes before commit (§5) |
| 8 | States move only from `PENDING` to `PROCESSED` or `FAILED` | Domain guard + SQL `AND state = 'PENDING'` (§5) |
| 9 | The ledger balances: total DEBIT = total CREDIT | Follows from 5 |
| 10 | Each wallet: `balance = opening_balance + credits − debits` | Follows from 5 and 6 |

Invariants 1–4 hold even if the code is wrong. Invariants 5–10 rely on the
transaction in §5, so the tests check them with reconciliation queries after
every scenario, including the concurrent ones (§11).

**Alternative considered:** deferred constraint triggers to enforce 5–7 in
the database. Rejected for this scope: trigger logic is harder to read and
test than one transaction plus reconciliation checks.

## 9. Failure Modes

| Failure | What happens | Client sees | Stored |
|---------|--------------|-------------|--------|
| Body larger than 64 KiB | Reading stops at the limit; rejected before any database work | `413` | Nothing |
| Invalid request (spec §6 steps 1–5) | Rejected before any database work | `400` | Nothing |
| Unknown wallet | FK violation on the key claim; rollback | `400 WALLET_NOT_FOUND` | Nothing |
| Key reused with a different body | Rollback | `409` | Nothing |
| Insufficient funds | Transfer committed as `FAILED` | `422` | The transfer only |
| Same request sent again | Key found; replay | Original response | Nothing new |
| Same key while the first is in flight | Waits on the key, then replays, or runs as new if the first rolled back | Original response | Nothing new |
| Database unreachable | The transaction cannot begin | `500` | Nothing |
| Statement fails mid-transaction (constraint, deadlock `40P01`) | Rollback | `500` | Nothing |
| Request exceeds 5 s (e.g. waiting on a lock) | Context cancelled; rollback | `500` | Nothing |
| Client disconnects before commit | Context cancelled; rollback | — | Nothing |
| Process crashes mid-transaction | Postgres rolls back when the connection drops | Connection error | Nothing |
| Commit succeeds, response lost | — | Connection error | Everything; a retry with the same key replays |
| `COMMIT` fails or its outcome is unknown | Treated as an error | `500` | All or nothing; a retry with the same key replays or runs as new, never twice |
| Schema or seed fails at startup | The service exits with a non-zero code | Service unavailable | — |

**Timeouts:** each request's context has a 5 s deadline, and the transaction
runs on that context. `http.Server` sets `ReadHeaderTimeout` (2 s),
`ReadTimeout` (5 s), and `WriteTimeout` (10 s, longer than the request
deadline so the error response can still be written).

**Shutdown:** on `SIGINT` or `SIGTERM`, the server stops accepting requests,
lets in-flight ones finish for up to 10 s, then closes the database pool.

## 10. Observability

The service logs with `log/slog`, as JSON to stdout (requirements NFR-5).

**One line per `POST /transfers` request**, written by the handler once the
response is decided. The service and repository do not log; they return
errors to the handler.

| Field            | When                     |
|------------------|--------------------------|
| `idempotencyKey` | when the request has one |
| `transferId`     | when a transfer exists   |
| `state`          | when a transfer exists   |
| `status`         | always (HTTP status)     |
| `error`          | `500` only               |

- `INFO` for `2xx` and `4xx` responses; `ERROR` for `500`.
- `cmd/server` also logs startup (listen address) and shutdown.
- No metrics (requirements §6).

```json
{"level":"INFO","msg":"transfer request","idempotencyKey":"abc123","transferId":"0192f3a4-5b6c-7d8e-9f01-23456789abcd","state":"PROCESSED","status":201}
```

## 11. Testing Strategy

`go test ./... -race -cover` runs every test with no external service
(requirements D-16).

### Levels

| Package            | Kind | Covers |
|--------------------|------|--------|
| `internal/domain`  | Unit, table-driven | Validation rules (spec §6 steps 3–5), state transitions |
| `internal/apitest` | API: HTTP → handler → service → repository → embedded Postgres | Everything else, asserted against the spec: status codes, bodies, and database state; also the startup schema and seed, applied as `cmd/server` applies them |

There are no mocks. API tests run the real layers, so they test the contract
rather than internals.

### Setup

- `TestMain` starts one embedded Postgres on a free port with a temporary data
  directory, applies `schema.sql`, and stops it after the run.
- Each test creates its own wallets and keys with unique IDs, so tests run in
  parallel (`t.Parallel()`) with no cleanup.
- After every test, a helper runs reconciliation checks for invariants 5–10
  (§8).

### Scenarios

| Area | Scenarios |
|------|-----------|
| Transfer execution | Sufficient funds → `201 PROCESSED`, balances moved, two ledger entries; amount equal to the balance → balance 0 |
| Failures | Insufficient funds → `422 FAILED`, no entries, balances unchanged; every `400` code in spec §4; a body over 64 KiB → `413`; check order (missing field + bad amount → `INVALID_FIELD`); database unavailable → `500`, nothing stored |
| Idempotency | Replaying a `PROCESSED` or `FAILED` transfer returns the identical response and adds no rows; different body → `409`; a rejected request does not use up its key; replay after restarting the service (new pool, same database) |
| Concurrency | 50 concurrent debits of 100 from a wallet holding 1000 → exactly 10 `PROCESSED`, balance 0; transfers in both directions between two wallets → no deadlock; 20 concurrent requests with one key → one transfer, 20 identical responses |
| Observability | One log line per request with the §10 fields: `201` → `INFO` with `idempotencyKey`, `transferId`, `state`, `status`; `500` → `ERROR` with `idempotencyKey`, `status`, `error` |
| Startup | Applying the schema and seed twice succeeds; the seed wallets have their §4 opening balances; applying both again after a transfer leaves every balance unchanged |

### Workflow

Each behavior lands as two commits: the failing test (Red), then the smallest
code that passes it (Blue). Refactoring follows with tests kept passing
(Green) (requirements NFR-4).
