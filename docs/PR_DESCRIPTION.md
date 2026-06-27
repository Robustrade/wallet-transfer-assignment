<!--
Ready-to-paste PR body. Fills every section of .github/pull_request_template.md.
-->

## Summary

Implements the wallet-transfer service: an HTTP API to move money between
wallets with **exactly-once** semantics, **safe concurrency** (no double-spend,
no deadlock), and a **double-entry ledger** for auditability. Built in clean,
layered ports-and-adapters style (domain → service → handler/repository),
backed by PostgreSQL via pgx/v5, with embedded migrations applied on startup and
graceful shutdown. Money is `int64` minor units end to end — no floating point.

Full design rationale: [`docs/DESIGN.md`](docs/DESIGN.md). Run/test instructions:
[`README.md`](README.md).

## AI disclosure

1. **Tool:** Claude Code (Anthropic), driving Claude (Opus-class) models — the
   only AI tool used.
2. **How it was used:** Documentation-first and contract-driven. The design
   (API, schema, idempotency, concurrency) was written before code; a shared
   domain/ports foundation was fixed first; independent layers (repository,
   handler, docs) were implemented in parallel while the author/orchestrator
   owned the domain foundation, the service orchestration, the `cmd/walletd`
   wiring, and the end-to-end concurrency tests. TDD throughout. All generated
   code, config, lint, and tests were reviewed and verified by the author.
3. **Transcript:** The full session transcript will be attached to the repo or
   emailed with this submission. This PR, [`docs/AI_USAGE.md`](docs/AI_USAGE.md),
   and the commit history also capture the prompts and workflow.

See [`docs/AI_USAGE.md`](docs/AI_USAGE.md) for the detailed disclosure.

## Schema Design

PostgreSQL, money as `BIGINT` minor units with invariants enforced by `CHECK`
constraints at the database boundary.

- **`wallets`** — `id TEXT PK`, `balance BIGINT CHECK (balance >= 0)`,
  timestamps. The non-negative check is a last-line overdraft guard.
- **`transfers`** — `id UUID PK`, `idempotency_key TEXT UNIQUE`,
  `from_wallet_id`/`to_wallet_id FK → wallets`, `amount BIGINT CHECK (> 0)`,
  `status CHECK IN (PENDING,PROCESSED,FAILED)`, `failure_reason`, timestamps,
  `CHECK (from <> to)`. Indexed on `from_wallet_id` and `to_wallet_id`.
- **`ledger_entries`** — `id UUID PK`, `wallet_id FK`, `transfer_id FK`,
  `type CHECK IN (DEBIT,CREDIT)`, `amount BIGINT CHECK (> 0)`,
  `UNIQUE (transfer_id, type)` (at most one DEBIT and one CREDIT per transfer).
  Indexed on `wallet_id` and `transfer_id`.
- **`idempotency_records`** — `key TEXT PK`, `request_hash`, `transfer_id FK`,
  `response_body JSONB`, `status CHECK IN (PENDING,COMPLETED)`, timestamps.

Migrations are embedded in the binary and applied idempotently on startup
(tracked in `schema_migrations`). Per-constraint rationale is in
[`docs/DESIGN.md`](docs/DESIGN.md) §3.

## Idempotency Strategy

Each `POST /transfers` carries a client `idempotencyKey`. The service claims it
by inserting an `idempotency_records` row **inside the transfer transaction**;
the `key` PRIMARY KEY is the single serialization point.

- First request wins the insert, executes the transfer, stores a response
  snapshot, marks `COMPLETED`.
- A concurrent duplicate **blocks on the unique index** until the first
  transaction commits, then observes the conflict and **replays** the stored
  result — money moves exactly once. Replays return `Idempotent-Replayed: true`.
- Durable, so retries after a restart still replay correctly.
- Same key + different payload (hash mismatch) → `409`.

Mechanics in [`docs/DESIGN.md`](docs/DESIGN.md) §4.

## Concurrency Strategy

One transfer runs in one transaction. Both wallets are locked with
`SELECT ... FOR UPDATE` acquired in **sorted wallet-id order**, which makes
opposing transfers (`A→B` / `B→A`) deadlock-free. Funds are checked under the
lock, both balances updated, the DEBIT/CREDIT ledger pair written, and the
transfer flipped to `PROCESSED` — atomically. Insufficient funds commits a
durable `FAILED` transfer (an idempotent failure) returned as `422`. Pessimistic
locking was chosen over optimistic concurrency for this write-heavy,
correctness-critical path; the tradeoff is discussed in
[`docs/DESIGN.md`](docs/DESIGN.md) §5.

## How to Run

- **Docker Compose:** `make compose-up` (builds the app, starts
  `postgres:16-alpine`, waits for `pg_isready`, runs migrations on startup).
  API on `http://localhost:8080`. Tear down with `make compose-down`.
- **Local:** set `DATABASE_URL` and `make run` (`go run ./cmd/walletd`).
- Config via env: `DATABASE_URL` (required), `HTTP_ADDR` (`:8080`),
  `READ_TIMEOUT`/`WRITE_TIMEOUT` (`10s`), `SHUTDOWN_TIMEOUT` (`15s`).
- A copy-paste demo (seed wallets, transfer, replay, read balance) is in the
  [README](README.md#quick-demo).

## How to Test

- `make test` — `go test ./... -race -cover` (unit + integration).
- `make test-integration` — Postgres-backed repository/concurrency tests.
- `make lint` / `make fmt-check` / `make vet` — the CI gates locally.
- **Unit tests** (domain, service-with-fakes, handler) need nothing external.
  **Integration tests** start a real `postgres:16-alpine` via testcontainers and
  **require Docker**; when Docker is unavailable they **self-skip**, so the suite
  is green without Docker and fully exercised with it.

## Tradeoffs / Assumptions

- Single currency; `int64` minor units (no floats).
- No authentication / authorization (out of scope).
- Wallets are pre-provisioned (seeded) — there is **no wallet-creation
  endpoint** by design.
- Pessimistic `FOR UPDATE` locking trades hot-wallet parallelism for simplicity
  and obvious correctness.
- Health endpoint is liveness-only; metrics and readiness are noted as next
  steps.

Full list in [`docs/DESIGN.md`](docs/DESIGN.md) §10.

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
