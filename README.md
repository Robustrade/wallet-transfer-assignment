# Wallet Transfer Service

A small, production-shaped HTTP service for moving money between wallets. It is
built around three hard requirements that make this problem interesting:

- **Correctness under concurrency** — concurrent transfers can never oversend or
  corrupt a balance, and opposing transfers can never deadlock.
- **Exactly-once semantics** — a client may safely retry a `POST /transfers`; a
  duplicate request (same idempotency key) replays the original result instead
  of moving money twice, and this survives process restarts.
- **Auditability** — every successful transfer writes a balanced double-entry
  ledger (one DEBIT, one CREDIT), so balances are always reconstructable.

Money is represented as `int64` minor units (e.g. cents). There is no floating
point anywhere in the money path.

The full design rationale lives in [`docs/DESIGN.md`](docs/DESIGN.md); the AI
usage disclosure is in [`docs/AI_USAGE.md`](docs/AI_USAGE.md).

---

## Architecture

Clean, layered ports-and-adapters. Dependencies point inward; the `service`
layer owns its interfaces and knows nothing about HTTP or SQL.

```
        ┌──────────────────────────────────────────────────────┐
        │                  cmd/walletd (main)                   │
        │   config load · open pool · migrate · serve · drain   │
        └───────────────┬──────────────────────┬───────────────┘
                        │                      │
                        ▼                      ▼
        ┌───────────────────────┐   ┌──────────────────────────┐
        │   internal/handler    │   │  internal/repository/pg   │
        │  chi router, strict   │   │  pgx/v5 adapters,         │
        │  JSON, domain→HTTP    │   │  embedded migrations,     │
        │  error mapping        │   │  DB is also the TxManager │
        └───────────┬───────────┘   └─────────────┬────────────┘
                    │                              │
                    │      implements ports        │
                    ▼                              ▼
        ┌──────────────────────────────────────────────────────┐
        │                   internal/service                    │
        │  TransferService use case. Owns the repository &      │
        │  TxManager interfaces. Orchestrates one transfer in   │
        │  one transaction with exactly-once semantics.         │
        └───────────────────────────┬──────────────────────────┘
                                    │
                                    ▼
        ┌──────────────────────────────────────────────────────┐
        │                    internal/domain                    │
        │  Wallet · Transfer · LedgerEntry · IdempotencyRecord  │
        │  money (int64 minor units) · state machine · errors   │
        └──────────────────────────────────────────────────────┘
```

- `cmd/walletd/main.go` — entrypoint: load config, open Postgres pool, run
  migrations, serve HTTP, graceful shutdown on SIGINT/SIGTERM.
- `internal/domain` — pure entities, the money type, the transfer state machine,
  and sentinel errors. No I/O.
- `internal/service` — the `TransferService` use case plus the repository and
  `TxManager` ports it depends on.
- `internal/repository/postgres` — pgx/v5 adapters; `DB` doubles as the
  `TxManager`; embedded, versioned migrations applied on startup.
- `internal/handler` — chi handlers, strict JSON decoding, domain-error → HTTP
  status mapping.
- `internal/config` — environment-driven configuration.

---

## Data model

PostgreSQL. All money columns are `BIGINT` minor units with `CHECK` constraints
that enforce invariants at the database boundary.

| Table                  | Key columns / constraints                                                                                                                                                              |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `wallets`              | `id TEXT PK`, `balance BIGINT CHECK (balance >= 0)`, timestamps. The non-negative check is a last-line defense against overdraft.                                                     |
| `transfers`            | `id UUID PK`, `idempotency_key TEXT UNIQUE`, `from_wallet_id`/`to_wallet_id TEXT FK → wallets`, `amount BIGINT CHECK (> 0)`, `status CHECK IN (PENDING,PROCESSED,FAILED)`, `failure_reason`, `CHECK (from <> to)`. |
| `ledger_entries`       | `id UUID PK`, `wallet_id FK`, `transfer_id FK`, `type CHECK IN (DEBIT,CREDIT)`, `amount BIGINT CHECK (> 0)`, `UNIQUE (transfer_id, type)` — at most one DEBIT and one CREDIT per transfer. |
| `idempotency_records`  | `key TEXT PK`, `request_hash`, `transfer_id FK`, `response_body JSONB`, `status CHECK IN (PENDING,COMPLETED)`, timestamps.                                                            |

Indexes: `transfers(from_wallet_id)`, `transfers(to_wallet_id)`,
`ledger_entries(wallet_id)`, `ledger_entries(transfer_id)`.

Migrations are embedded in the binary and applied idempotently on startup
(tracked in a `schema_migrations` table), so a fresh database needs no manual
setup.

---

## Idempotency (exactly-once)

Each `POST /transfers` carries a client-supplied `idempotencyKey`. The service
claims that key by `INSERT`-ing an `idempotency_records` row inside the same
transaction that performs the transfer. The `key` PRIMARY KEY (backed
defensively by the `UNIQUE` on `transfers.idempotency_key`) does the heavy
lifting:

- **First request** wins the insert, executes the transfer, stores an auditable
  response snapshot, and marks the record `COMPLETED`.
- **Concurrent duplicate** blocks on the unique constraint until the first
  transaction commits, then observes the conflict and **replays** the stored
  result rather than re-executing — money moves exactly once.
- **Retry after completion** finds the existing record and replays it. Responses
  to replays carry the header `Idempotent-Replayed: true`.
- **Restart safe** — the record is durable, so a retry after a crash still
  replays correctly.
- **Same key, different payload** is a client mistake and returns `409 Conflict`
  (the stored `request_hash` does not match).

See [`docs/DESIGN.md`](docs/DESIGN.md) for the in-flight blocking mechanics.

---

## Concurrency (no double-spend, no deadlock)

One transfer runs in exactly one database transaction:

1. Lock both wallets with `SELECT ... FOR UPDATE`, acquired in **sorted wallet-id
   order**. Deterministic lock ordering means two opposing transfers
   (`A→B` and `B→A`) can never deadlock.
2. Check funds, update both balances, write the DEBIT/CREDIT ledger pair, and
   flip the transfer to `PROCESSED` — all atomically.
3. **Insufficient funds** is not an exception: the transaction commits a durable
   `FAILED` transfer (an idempotent failure) and the API returns `422`.

Pessimistic row locks are used deliberately over optimistic concurrency; the
tradeoff is discussed in [`docs/DESIGN.md`](docs/DESIGN.md).

---

## API reference

Base URL defaults to `http://localhost:8080`. All bodies are JSON. Errors use a
single envelope:

```json
{ "error": { "code": "insufficient_funds", "message": "insufficient funds" } }
```

### `POST /transfers`

Create (or replay) a transfer.

```json
{ "idempotencyKey": "abc", "fromWalletId": "w1", "toWalletId": "w2", "amount": 100 }
```

| Status | Meaning                                                                  |
| ------ | ------------------------------------------------------------------------ |
| `201`  | Fresh transfer processed.                                                |
| `200`  | Idempotent replay (header `Idempotent-Replayed: true`).                  |
| `422`  | Insufficient funds — body contains the durable `FAILED` transfer + error.|
| `400`  | Invalid request (bad JSON, missing fields, non-positive amount, same wallet). |
| `409`  | Idempotency key reused with a different payload.                         |

### `GET /transfers/{id}`

Fetch a transfer by id → `200` with the transfer, or `404`.

### `GET /wallets/{id}/balance`

→ `200` `{"walletId":"w1","balance":900}` or `404`.

### `GET /healthz`

→ `200` `{"status":"ok"}`.

> **Note: there is no wallet-creation endpoint.** Wallets are pre-provisioned
> (seeded) out of band — see the assumptions in `docs/DESIGN.md`. For a demo,
> seed them with SQL as shown below.

---

## Quick demo

With the stack running (see *How to run*), seed two wallets, transfer, retry to
see the replay, then read the balance.

```bash
# 1. Seed two wallets directly in Postgres (no API for this by design).
psql postgres://wallet:wallet@localhost:5432/wallet -c \
  "INSERT INTO wallets (id, balance) VALUES ('w1', 1000), ('w2', 0)
   ON CONFLICT (id) DO NOTHING;"

# 2. Transfer 100 minor units w1 -> w2. Expect 201.
curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-1","fromWalletId":"w1","toWalletId":"w2","amount":100}'

# 3. Replay the SAME request. Expect 200 + "Idempotent-Replayed: true". No double spend.
curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-1","fromWalletId":"w1","toWalletId":"w2","amount":100}'

# 4. Read balances. w1 == 900, w2 == 100.
curl -s http://localhost:8080/wallets/w1/balance
curl -s http://localhost:8080/wallets/w2/balance
```

---

## How to run

### Docker Compose (recommended)

Brings up Postgres + the app. The app runs its own migrations on startup.

```bash
make compose-up        # docker compose -f deployments/docker-compose.yml up --build
# ... then use the API on http://localhost:8080
make compose-down      # tear down and remove volumes
```

### Local (Go + your own Postgres)

```bash
export DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
make run               # go run ./cmd/walletd
```

Configuration (environment variables, read by `internal/config`):

| Variable           | Required | Default | Notes                                   |
| ------------------ | -------- | ------- | --------------------------------------- |
| `DATABASE_URL`     | yes      | —       | pgx/libpq connection string.            |
| `HTTP_ADDR`        | no       | `:8080` | Listen address.                         |
| `READ_TIMEOUT`     | no       | `10s`   | Request read timeout.                   |
| `WRITE_TIMEOUT`    | no       | `10s`   | Response write timeout.                 |
| `SHUTDOWN_TIMEOUT` | no       | `15s`   | Graceful shutdown budget.               |

---

## How to test

```bash
make test              # go test ./... -race -cover  (unit + integration)
make test-integration  # Postgres-backed tests explicitly (repository + e2e)
make lint              # golangci-lint run ./...
make fmt-check         # gofmt gate used by CI
make vet               # go vet ./...
```

- **Unit tests** (domain, service with fakes, handler) need nothing external.
- **Integration tests** spin up a real `postgres:16-alpine` via
  [testcontainers](https://golang.testcontainers.org/). Two layers: the
  repository tests (`internal/repository/postgres`) verify real `FOR UPDATE`
  lock-blocking and the schema constraints; the end-to-end tests
  (`test/integration`) drive the full HTTP→service→Postgres stack to prove **no
  double-spend** under 25 concurrent debits and **exactly-once** under 25
  concurrent duplicate requests. **They require Docker.** When no Docker host is
  reachable, the suites **self-skip** instead of failing — so `make test` is
  green on a machine without Docker, and fully exercised on one with it.

CI runs Go 1.24 with `go test ./... -race -cover` (the race detector needs cgo,
so `CGO_ENABLED=1`).

---

## Project layout

```
.
├── cmd/walletd/                  # main: config, pool, migrate, serve, shutdown
├── internal/
│   ├── config/                   # env-driven configuration
│   ├── domain/                   # entities, money, state machine, errors (no I/O)
│   ├── service/                  # TransferService use case + ports it owns
│   ├── repository/postgres/      # pgx/v5 adapters + embedded migrations
│   └── handler/                  # chi HTTP handlers, strict JSON, error mapping
├── test/integration/            # end-to-end concurrency & idempotency tests
├── deployments/docker-compose.yml
├── docs/                         # DESIGN.md, AI_USAGE.md, PR_DESCRIPTION.md
├── Dockerfile
├── Makefile
└── .golangci.yml
```

---

## Design decisions & tradeoffs (summary)

- **Integer minor units, never floats** — exact arithmetic; DB `CHECK`s reject
  bad amounts.
- **Pessimistic `FOR UPDATE` + sorted lock order** — simple, correct,
  deadlock-free; chosen over optimistic retries for write-heavy correctness.
- **Idempotency enforced by a unique constraint**, not an application lock —
  correct across processes and restarts, with replay of a stored snapshot.
- **Failures are durable** — insufficient funds commits a `FAILED` transfer so a
  retry is idempotent and observable, rather than vanishing.
- **Single currency, no auth, wallets pre-provisioned** — scope deliberately
  trimmed to the assignment's core; see `docs/DESIGN.md` for the full list.

For depth on any of the above, read [`docs/DESIGN.md`](docs/DESIGN.md). AI usage
is disclosed in [`docs/AI_USAGE.md`](docs/AI_USAGE.md).
