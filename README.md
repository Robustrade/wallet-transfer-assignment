# Wallet Transfer Service

A small, production-minded service for **wallet-to-wallet transfers** in Go. It is
built around four guarantees the assignment asks for:

- **Idempotent request handling** — exactly-once at the API level per `idempotencyKey`.
- **Double-entry ledger** — every processed transfer writes one balanced DEBIT/CREDIT pair.
- **Correct balances under concurrency** — no double spend, no lost updates.
- **Safe state transitions** — `PENDING → PROCESSED` or `PENDING → FAILED`, once.

> The implementation favours **correctness and clarity** over feature breadth, following
> SOLID, KISS and YAGNI.

---

## Table of contents

- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [API](#api)
- [Database schema](#database-schema)
- [Idempotency strategy](#idempotency-strategy)
- [Concurrency strategy](#concurrency-strategy)
- [Testing](#testing)
- [Project layout](#project-layout)
- [Design decisions & tradeoffs](#design-decisions--tradeoffs)
- [Submission](#submission)

---

## Architecture

A clean, layered architecture with strict dependency direction (outer depends on inner;
the inner core knows nothing about HTTP or SQL):

```text
            HTTP request
                 │
        ┌────────▼─────────┐   transport/http
        │     Handler      │   validation, JSON, status mapping  (thin)
        └────────┬─────────┘
                 │ domain types
        ┌────────▼─────────┐   service
        │     Service      │   workflow, idempotency, orchestration
        └────────┬─────────┘
                 │ repository interfaces (ports)
        ┌────────▼─────────┐   repository/{postgres,memory}
        │   Repository     │   persistence only — no business rules
        └────────┬─────────┘
                 │
        ┌────────▼─────────┐   domain
        │  Domain models   │   entities, state machine, validation (no I/O)
        └──────────────────┘
```

**Dependency Inversion** is the key idea: the service depends on repository *interfaces*
(`internal/repository`), not on a concrete database. Two implementations satisfy them:

- `repository/postgres` — production store using **pgx** + a connection pool, real
  `SELECT … FOR UPDATE` row locks and `INSERT … ON CONFLICT`.
- `repository/memory` — a dependency-free store that faithfully models row locking and the
  unique idempotency constraint, so the **concurrency and idempotency tests run in CI under
  `-race` with no external services**.

---

## Quick start

### Option A — run with no database (in-memory store)

Great for a quick look or running the example requests. Data is not persisted.

```bash
make run          # or: go run ./cmd/server
```

The server listens on `:8080`. Try it:

```bash
curl -s localhost:8080/healthz

# create two wallets — the server generates each id and returns it
src=$(curl -s -XPOST localhost:8080/api/v1/wallets \
  -d '{"openingBalance":1000,"currency":"USD"}' | jq -r .id)
dst=$(curl -s -XPOST localhost:8080/api/v1/wallets \
  -d '{"openingBalance":0,"currency":"USD"}' | jq -r .id)

# transfer 100 (idempotent on the key)
curl -s -XPOST localhost:8080/api/v1/transfers \
  -d "{\"idempotencyKey\":\"abc123\",\"fromWalletId\":\"$src\",\"toWalletId\":\"$dst\",\"amount\":100}"

# replay the exact same request -> same result, no second debit
curl -s -XPOST localhost:8080/api/v1/transfers \
  -d "{\"idempotencyKey\":\"abc123\",\"fromWalletId\":\"$src\",\"toWalletId\":\"$dst\",\"amount\":100}"

curl -s localhost:8080/api/v1/wallets/$src            # balance is 900, not 800
curl -s localhost:8080/api/v1/wallets/$src/ledger     # one DEBIT entry
```

See [`examples/requests.http`](./examples/requests.http) for a ready-to-run set (VS Code REST Client).

For a one-shot end-to-end check of **every** scenario (success, idempotent replay, key
conflict, insufficient funds, reads, and a concurrent same-key burst), run the smoke script
against a running server (requires `jq`):

```bash
make run      # in one shell
make smoke    # in another — or: ./examples/smoke.sh
```

### Option B — run with PostgreSQL (docker compose)

Runs the API plus PostgreSQL; the schema is applied automatically on startup.

```bash
make compose-up    # docker compose up --build
# ... then the same curl commands as above
make compose-down  # tear down
```

---

## Configuration

All configuration is via environment variables (12-factor); sensible defaults apply.

| Variable        | Default   | Description                                                        |
|-----------------|-----------|--------------------------------------------------------------------|
| `HTTP_ADDR`     | `:8080`   | HTTP listen address.                                               |
| `DATABASE_URL`  | *(empty)* | PostgreSQL DSN. **Empty → in-memory store** (local/demo only).     |
| `DB_MAX_CONNS`  | `10`      | Max pooled connections.                                            |

---

## API

Base path is versioned: **`/api/v1`**. All bodies are JSON. Amounts are **integer minor
units** (e.g. cents) to avoid floating-point error.

| Method & path                          | Purpose                         | Success           |
|----------------------------------------|---------------------------------|-------------------|
| `POST /api/v1/transfers`               | Create / replay a transfer      | `201` or `200`    |
| `GET  /api/v1/transfers/{id}`          | Fetch a transfer                | `200`             |
| `POST /api/v1/wallets`                 | Create a wallet                 | `201`             |
| `GET  /api/v1/wallets/{id}`            | Fetch a wallet (with balance)   | `200`             |
| `GET  /api/v1/wallets/{id}/ledger`     | Wallet ledger history           | `200`             |
| `GET  /healthz`                        | Liveness                        | `200`             |

### Create transfer

```http
POST /api/v1/transfers
Content-Type: application/json

{ "idempotencyKey": "abc123", "fromWalletId": "8f14e45f…", "toWalletId": "c9f0f895…", "amount": 100 }
```

- **`201 Created`** — first time this key is seen; transfer is `PROCESSED` (or `FAILED`).
- **`200 OK`** — duplicate key with identical parameters; the **original** result is replayed.
- **`409 Conflict`** — key reused with **different** parameters.
- **`400 Bad Request`** — validation failure (missing field, same wallet, non-positive amount, unknown JSON field).

> A business failure such as insufficient funds is **not** an HTTP error: it returns `201`
> with `status: "FAILED"` and a `failureReason`, because the request was handled correctly —
> the transfer simply did not move money. This keeps "transport failure" and "business
> outcome" cleanly separated.

Example processed response:

```json
{
  "id": "5e4c…",
  "idempotencyKey": "abc123",
  "fromWalletId": "8f14e45f…",
  "toWalletId": "c9f0f895…",
  "amount": 100,
  "currency": "USD",
  "status": "PROCESSED",
  "createdAt": "2026-06-07T10:00:00Z",
  "updatedAt": "2026-06-07T10:00:00Z"
}
```

Errors use a uniform envelope:

```json
{ "error": { "code": "IDEMPOTENCY_KEY_CONFLICT", "message": "idempotency key already used with different parameters" } }
```

---

## Database schema

Four concepts, three tables (the transfer row **is** the idempotency record):
`wallets`, `transfers`, `ledger_entries`. See
[`migrations/0001_init.sql`](./migrations/0001_init.sql) (and the embedded copy in
`internal/repository/postgres/schema.sql`).

**`wallets`**
- `id` PK, `balance BIGINT`, `currency`, timestamps.
- `CHECK (balance >= 0)` — the database itself forbids an overdraft (defence in depth).

**`transfers`**
- `id` PK, `idempotency_key`, `from_wallet_id`/`to_wallet_id` FKs, `amount`, `status`, `failure_reason`, timestamps.
- **`UNIQUE (idempotency_key)`** — the single source of truth for deduplication.
- `CHECK (amount > 0)`, `CHECK (from_wallet_id <> to_wallet_id)`, `CHECK (status IN (...))`.

**`ledger_entries`**
- `id` PK, `transfer_id` FK, `wallet_id` FK, `type` (`DEBIT`/`CREDIT`), `amount`, `created_at`.
- **`UNIQUE (transfer_id, type)`** — a transfer can have at most one debit and one credit,
  so reprocessing can never duplicate entries.

**Indexes** (read paths stay fast):
- `idx_ledger_wallet_id (wallet_id, id DESC)` — ledger history, newest first.
- `idx_transfers_from_wallet`, `idx_transfers_to_wallet` — transfer lookups by wallet.

---

## Idempotency strategy

1. The client supplies an `idempotencyKey`.
2. Inside one transaction the service does `INSERT … ON CONFLICT (idempotency_key) DO NOTHING
   RETURNING …` of a `PENDING` transfer.
   - **Row returned** → this caller owns the transfer and processes it.
   - **No row** → the key already exists; the stored transfer is loaded and **replayed**.
3. A replay with **identical parameters** returns the original result (`200`). A replay with
   **different parameters** returns `409` — we never silently apply a different transfer under
   a reused key.

Because the idempotency record and the transfer are the **same row**, there is no window where
"a record exists but the transfer is missing". The guarantee survives process restarts (it is
in the database) and concurrent duplicates (the unique index serialises them — exactly one
`INSERT` wins).

---

## Concurrency strategy

**Pessimistic row locking inside a single transaction.**

- The whole transfer (claim → lock → debit/credit → ledger → status) runs in **one DB
  transaction**: it commits atomically or rolls back entirely.
- Both wallets are locked with `SELECT … FOR UPDATE` **before** any balance is read, which
  closes the classic read-then-write race. Two concurrent debits on the same wallet are
  serialised; the second sees the balance the first already wrote.
- Locks are always acquired in a **deterministic (sorted) order**, so two transfers touching
  the same pair of wallets in opposite directions can never deadlock.
- `CHECK (balance >= 0)` is a final backstop even if application logic were wrong.

This is simple to reason about and correct under contention. Tradeoffs vs. optimistic locking
are discussed below.

---

## Testing

```bash
make test     # CGO_ENABLED=1 go test ./... -race -cover -count=1   (matches CI)
```

Tests are **behavioural** (they assert outcomes, not internals) and cover the critical paths:

- **Domain** — state machine (each illegal transition rejected), wallet debit/credit invariants,
  request validation, ledger pair balancing.
- **Service** — happy-path transfer, insufficient-funds → `FAILED` with no ledger rows,
  idempotent replay (no second debit), key-reuse conflict, and two concurrency tests run under
  the race detector:
  - 20 concurrent requests with the **same** key → exactly one debit, one transfer id.
  - 10 concurrent distinct debits on a wallet that can fund only 5 → exactly 5 `PROCESSED`,
    5 `FAILED`, balance drained to 0, never negative.
- **Transport** — full HTTP stack via `httptest`: `201` vs `200` on replay, `409` on conflict,
  `400` validation (including unknown-field rejection), ledger history, health.

> The in-memory store deliberately models row locks and the unique idempotency constraint, so
> these concurrency guarantees are exercised in CI without needing PostgreSQL.

Other useful targets: `make lint`, `make fmt-check`, `make vet`, `make cover`, `make check`.

---

## Project layout

```text
cmd/server/                 entrypoint: config, wiring, graceful shutdown
internal/
  config/                   env-based configuration
  domain/                   entities, state machine, validation (no I/O)
  repository/               ports (interfaces) + Store abstraction
    memory/                 in-memory store (models locks + unique key)
    postgres/               pgx store: pool, FOR UPDATE, ON CONFLICT, schema
  service/                  transfer & wallet use cases (business logic)
  transport/http/           handlers, DTOs, router, middleware, error mapping
migrations/                 SQL schema
examples/requests.http      runnable sample requests
```

---

## Design decisions & tradeoffs

- **Stored balance + ledger (not balance derived from the ledger).** O(1) balance reads and a
  natural `FOR UPDATE` lock point; the ledger remains the auditable source of truth. Deriving
  balances would make reads O(n) and complicate locking.
- **Pessimistic locking over optimistic.** For hot-wallet contention, retry storms under
  optimistic concurrency hurt; row locks give predictable behaviour and simpler code. Optimistic
  locking (a `version` column) would suit low-contention, high-throughput workloads.
- **Transfer row is the idempotency record.** Removes a whole class of "partial idempotency"
  bugs versus a separate `idempotency_records` table.
- **Server-generated UUID ids for wallets and transfers.** The server mints both ids (UUIDv4)
  rather than trusting client-supplied values: callers never collide or squat on an id, ids are
  not enumerable, and identity is consistent across both resources. New resources are advertised
  via the `Location` header. The id column stays `TEXT`, so swapping the generator (e.g. ULID for
  sortable ids) needs no schema change. Idempotency is still keyed on `idempotencyKey`, not the
  id, so retries remain safe.
- **Integer minor units** for money — no floating-point rounding.
- **Money never moves on FAILED** — failed transfers write no ledger entries, so the ledger is
  always balanced.
- **Two store implementations** add a little code, but buy fast, deterministic, dependency-free
  concurrency tests — a strong fit for this assignment.

Possible next steps (intentionally out of scope): transfer history pagination, a reversal/refund
flow, metrics (Prometheus) and tracing, and a dedicated migration tool for versioned rollouts.

---

## Submission

- Branch: `solution/<your-name>`, PR into `main`.
- The PR description covers schema, idempotency and concurrency (see the PR template) and the
  AI-usage note.

### How to run (recap)

```bash
make run          # in-memory, no dependencies
make compose-up   # API + PostgreSQL via docker compose
```

### How to test (recap)

```bash
make test         # go test ./... -race -cover
```