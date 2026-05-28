# Code Walkthrough (from `main` to the core workflow)

This document walks through the codebase from the process entrypoint down to the core transfer transaction and storage layer. It also calls out key tradeoffs, why each decision was chosen, and what the downsides are.

> Naming note: files are organized so that each module can be extracted into a microservice later. Most cross-layer calls are via interfaces (ports), while the concrete DB/HTTP implementations live in adapters.

## High-level structure

- `cmd/api/` — process entrypoint (minimal wiring)
- `internal/app/` — application bootstrap (DB, services, router, HTTP server, lifecycle)
- `internal/adapters/http/` — HTTP handlers + routing (thin)
- `internal/adapters/postgres/` — repositories (SQL only, no orchestration)
- `internal/transfers/` — transfer domain + orchestrating service
- `internal/wallets/` — wallet domain + service
- `internal/ledger/` — (kept inside repo layer for now; ledger write port is in transfers)
- `internal/idempotency/` — idempotency interfaces + request hashing + cache decorator
- `internal/cache/` — small in-memory LRU+TTL cache implementation
- `internal/platform/` — config + shared HTTP utils
- `db/migrations/` — schema

## Entry → App bootstrap

### `cmd/api/main.go`

Purpose:

- Reads config from env.
- Builds the app via `app.New(...)`.
- Runs the server until SIGINT/SIGTERM via `signal.NotifyContext`.

Why:

- Keeps `main` small and stable.
- Makes it easier to test/init the app in other contexts later (e.g., CLI tools, benchmarks).

### `internal/app/app.go`

Purpose:

- Connects to Postgres (`pgxpool.New`) and waits for readiness.
- Constructs repositories and services.
- Builds the router with middleware and registers endpoints.
- Runs the HTTP server + shutdown coordination using an `errgroup`.

Concurrency usage:

- Two goroutines coordinated by `errgroup`:
  - server goroutine runs `ListenAndServe`
  - shutdown goroutine waits for `ctx.Done()` and performs `Shutdown` + DB close

Tradeoffs:

- This is “single-process reliable” but not yet full production lifecycle management (no metrics, tracing, readiness/liveness split, etc.).
- Server failures trigger best-effort cleanup, but no automatic restarts (left to infra like systemd/Kubernetes).

## HTTP layer (thin handlers)

### `internal/adapters/http/routes.go`

Purpose:

- Defines routes:
  - `POST /wallets`
  - `GET /wallets/{walletId}`
  - `POST /transfers`
- Decodes JSON with `DisallowUnknownFields` for stricter API behavior.
- Converts service results into HTTP responses.

Tradeoffs:

- Handlers intentionally do not “know” about SQL or transactional behavior.
- Error mapping is intentionally simple (assignment-focused); production systems would typically standardize error codes + structured error details.

## Core transfer workflow (the “money move”)

### `internal/transfers/transfers.go`

Purpose:

- Declares transfer states: `PENDING`, `PROCESSED`, `FAILED`.
- Defines request/response DTOs for the service layer.
- Defines error types that represent business outcomes.

### `internal/transfers/service.go`

Purpose:

- Implements the entire transfer workflow in a **single explicit DB transaction**.
- Enforces:
  - idempotency and exact replay
  - safe state transitions
  - no double spend
  - double-entry ledger write

Step-by-step (happy path):

1) Validate request (basic invariants).
2) Begin DB transaction.
3) **Idempotency gate**:
   - `TryInsert(idempotencyKey, requestHash)`:
     - if existing with different hash → `409`
     - if existing and finalized → replay stored `{status, body}`
     - if inserted → proceed with execution
4) Insert `transfers` row as `PENDING`.
5) Lock both wallet rows deterministically (lexicographic wallet_id) to reduce deadlock risk when two transfers overlap wallet pairs.
6) Conditional atomic debit:
   - `UPDATE wallets SET balance = balance - amount WHERE wallet_id = from AND balance >= amount`
   - if affected rows = 0 → insufficient funds:
     - mark transfer `FAILED`
     - store idempotency response
     - commit and replay on future duplicates
7) Credit destination wallet.
8) Insert 2 ledger entries (DEBIT + CREDIT) for the transfer.
9) Mark transfer `PROCESSED`.
10) Store idempotency response and commit.

Why conditional debit:

- It collapses “check funds + debit” into one atomic statement, preventing read-then-write races under concurrency.

Where it can fail / downsides:

- Under extreme contention, lock waits can increase latency.
- Postgres can still raise deadlocks in pathological multi-row lock patterns; Postgres will abort one transaction. Today the code returns a 500 in that case; a production system usually adds bounded retries on deadlock/serialization errors.
- The stored idempotency response is persisted in DB; in-memory cache is a best-effort optimization only.

## Idempotency module (durable + cached)

### `internal/idempotency/idempotency.go`

Purpose:

- Defines the `Store` interface and record model.
- Computes `request_hash` for `{from,to,amount}`.
- Implements `CachedStore` decorator:
  - read-through cache for finalized responses
  - `singleflight` to reduce stampedes for hot keys

Tradeoffs:

- Cache is intentionally “non-authoritative”: eviction does not change correctness; it only affects latency.
- Cache TTL must not be interpreted as retention policy; DB is the retention layer.

## Postgres repositories (persistence only)

### `internal/adapters/postgres/wallet_repository.go`

Purpose:

- Wallet CRUD.
- Transactional helpers used by transfer service:
  - `LockTx(... FOR UPDATE)` to lock specific wallet rows
  - `DebitIfSufficientTx(...)` conditional debit
  - `CreditTx(...)` credit update

Tradeoffs:

- Uses row locks for deterministic lock ordering; this is a conservative deadlock-avoidance measure when many transfers touch overlapping wallet pairs.

### `internal/adapters/postgres/transfer_repository.go`

Purpose:

- Inserts transfer row (`PENDING`) and updates transfer state to `PROCESSED`/`FAILED`.

State machine enforcement:

- The service is the single orchestrator of state transitions.
- The DB also constrains allowed states via `CHECK (state IN (...))`.

### `internal/adapters/postgres/ledger_repository.go`

Purpose:

- Inserts the two ledger entries in a single statement.
- Validates `RowsAffected() == 2` as an extra guard.

DB guarantees:

- `UNIQUE(transfer_id, entry_type)` prevents duplicate DEBIT/CREDIT for the same transfer.
- `ledger_entries.transfer_id` FK ensures ledger entries cannot exist without a transfer.

### `internal/adapters/postgres/idempotency_repository.go`

Purpose:

- Durable idempotency storage and replay payload persistence.

Important concurrency choice:

- Uses:
  - `INSERT ... ON CONFLICT (idempotency_key) DO UPDATE ... RETURNING ...`

Why:

- Ensures duplicate requests serialize on the idempotency row, and the “loser” can safely see the finalized replay response after the “winner” commits (avoids the classic `DO NOTHING + SELECT` visibility race under concurrency).

## Cache implementation

### `internal/cache/lru_ttl.go`

Purpose:

- Simple in-memory best-effort cache:
  - max entries (LRU)
  - TTL per item
- Used for idempotency replay acceleration.

Tradeoffs:

- This is per-process memory; in multi-instance deployments you’d typically use Redis or accept cache misses.
- Not used for correctness decisions (e.g. balance checks).

## Platform utilities

### `internal/platform/config/config.go`

Purpose:

- Loads `DATABASE_URL` and `PORT` from env.

### `internal/platform/httputil/*`

Purpose:

- JSON content-type middleware
- helpers to write JSON success/error responses

## Schema

### `db/migrations/001_init.up.sql`

Key invariants:

- `wallets.balance >= 0`
- `transfers.state` constrained to `PENDING|PROCESSED|FAILED`
- `transfers.idempotency_key UNIQUE`
- `ledger_entries UNIQUE(transfer_id, entry_type)` and FK to `transfers`
- `idempotency_records` stores `{request_hash, response_code, response_body}` for exact replay

## Tests (evaluation coverage)

### `internal/transfers/service_integration_test.go`

Contains evaluation-focused tests for:

- idempotency replay stability
- idempotency mismatch (409)
- insufficient funds → FAILED + replay stable + no ledger
- concurrent debits → no double spend + ledger count matches processed transfers
- concurrent same idempotency key → single transfer + two ledger rows + same response

These tests are integration-style and require a reachable Postgres (they skip unless `TEST_DATABASE_URL` or `DATABASE_URL` is set).

## Sequence diagram (full flow for `POST /transfers`)

```mermaid
sequenceDiagram
  autonumber
  participant C as Client
  participant H as HTTP Handler
  participant S as TransferService
  participant I as IdempotencyStore(DB)
  participant W as WalletRepo(DB)
  participant T as TransferRepo(DB)
  participant L as LedgerRepo(DB)

  C->>H: POST /transfers (idempotencyKey, from, to, amount)
  H->>S: Create(req)

  S->>S: Validate request
  S->>S: BEGIN TX

  S->>I: TryInsert(key, requestHash) (UPSERT + RETURNING)
  alt Existing finalized + same hash
    I-->>S: {ready=true, status, body}
    S->>S: ROLLBACK
    S-->>H: replay {status, body}
    H-->>C: HTTP {status, body}
  else Existing + hash mismatch
    I-->>S: {ready=?, requestHash differs}
    S->>S: ROLLBACK
    S-->>H: 409 Conflict
    H-->>C: 409
  else Inserted new idempotency record
    I-->>S: inserted=true
    S->>T: INSERT transfer (PENDING)
    S->>W: LockTx(from) FOR UPDATE
    S->>W: LockTx(to) FOR UPDATE
    alt Wallet missing
      S->>T: UPDATE transfer FAILED
      S->>I: SetResponse(404, body)
      S->>S: COMMIT
      S-->>H: 404
      H-->>C: 404
    else Wallets exist
      S->>W: DebitIfSufficientTx(from, amount)
      alt Insufficient funds (0 rows)
        S->>T: UPDATE transfer FAILED
        S->>I: SetResponse(409, body)
        S->>S: COMMIT
        S-->>H: 409
        H-->>C: 409
      else Debited
        S->>W: CreditTx(to, amount)
        S->>L: INSERT 2 ledger rows (DEBIT, CREDIT)
        S->>T: UPDATE transfer PROCESSED
        S->>I: SetResponse(201, body)
        S->>S: COMMIT
        S-->>H: 201 {transferId, state}
        H-->>C: 201
      end
    end
  end
```

