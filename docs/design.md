# Wallet Transfer Service — Design

> Status: **implemented**. Written before the code (documentation-first) and kept in sync as each
> phase landed. Deviations from the original brief are called out inline.

## 1. Problem

Move money between two wallets through `POST /transfers` so that:

- a request with a given `idempotencyKey` has its effect **at most once** and
  always returns the **same result** when retried (exactly-once at the API level);
- every processed transfer writes a **balanced double-entry ledger** (one DEBIT, one CREDIT);
- balances stay **correct under concurrency**: no double spend, never negative;
- a transfer is **all-or-nothing**;
- transfer status follows a **safe state machine**: `PENDING -> PROCESSED | FAILED`.

The service runs on SQLite (default, zero setup) or PostgreSQL, selected by `DB_DRIVER`.

## 2. API contract

### `POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

Validation (400 `VALIDATION_ERROR`, nothing persisted):

| Field | Rule |
|---|---|
| body | `Content-Type: application/json` (parameters such as `charset` allowed); exactly one JSON object; no unknown fields; nothing after the object |
| `idempotencyKey` | required, 1–128 chars, `^[A-Za-z0-9._:-]+$`, case-sensitive |
| `fromWalletId`, `toWalletId` | required, 1–64 chars, `^[A-Za-z0-9._:-]+$`, must differ |
| `amount` | required JSON integer, `1 <= amount <= 100000000` (minor units); floats, strings, `null`, exponents rejected |

Wallet IDs use the same character set as idempotency keys. This keeps the
request hash unambiguous (§5) and makes "chars" mean bytes (ASCII only).

Response (same shape for every transfer outcome, including replays):

```json
{ "id": "0192…", "idempotencyKey": "abc123", "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2", "amount": 100, "status": "PROCESSED",
  "failureReason": null, "createdAt": "2026-09-26T10:00:00.000000Z" }
```

| Case | HTTP | Code |
|---|---|---|
| New transfer PROCESSED | 201 | — |
| Transfer FAILED (business rule) | 422 | body is the FAILED transfer |
| Replay | same as original | header `Idempotent-Replayed: true` |
| Validation | 400 | `VALIDATION_ERROR` |
| Body > `MAX_BODY_BYTES` | 413 | `PAYLOAD_TOO_LARGE` |
| Wallet missing | 404 | `WALLET_NOT_FOUND` |
| Same key, different payload | 409 | `IDEMPOTENCY_KEY_REUSED` |
| Timeout / lock timeout / retries exhausted | 503 | `TEMPORARILY_UNAVAILABLE` |
| Anything else | 500 | `INTERNAL_ERROR` (no internal text leaked) |

Errors: `{ "error": { "code", "message", "requestId" } }`. Every response carries `X-Request-ID`.

Unknown routes return 404 with code `NOT_FOUND`. This code was added beyond
the original brief, so that every response is JSON.

The HTTP status is **derived from the stored transfer status** (PROCESSED → 201,
FAILED → 422), so a replay returns the original status without storing the
response.

### Read APIs

- `GET /wallets/:id` → `{ id, balance, currency, updatedAt }` or 404.
- `GET /wallets/:id/transfers?limit=20` → `{ "transfers": [...] }`, where the wallet is
  sender or receiver, newest first (`created_at DESC, id DESC`); `limit` 1–100, else 400.
  An unknown wallet is 404, not an empty list, so a typo is not mistaken for "no activity".
- `GET /healthz` (process only), `GET /readyz` (pings DB; 503 if down).

### Business rules (FAILED, stored, replayable)

Checked in this order:

| Rule | `failureReason` |
|---|---|
| wallets have different currencies | `CURRENCY_MISMATCH` |
| source balance < amount | `INSUFFICIENT_FUNDS` |
| destination balance + amount overflows int64 | `BALANCE_OVERFLOW` |

`amount == balance` is allowed and leaves the source at 0.

## 3. Architecture

```
handler  (Gin, DTOs, validation, error→HTTP mapping)
   │  context.Context + service.TransferCommand
service  (idempotency, orchestration, transaction boundary)
   │  ports: TxManager, Repos
repository/sqlrepo  (SQL written once with "?" placeholders)
   │  Dialect (sqlite | postgres)
database
domain  (pure Go: Wallet, Transfer state machine, LedgerEntry, sentinel errors)
```

Rules: handlers are thin; SQL exists only in the repository layer; Gin types
exist only in `handler`/`middleware`; the service receives `context.Context`.

## 4. Schema

Money is `int64` minor units everywhere (SQLite `INTEGER`, Postgres `BIGINT`).

| Table | Key columns and constraints |
|---|---|
| `wallets` | `id` PK, `balance CHECK (balance >= 0)`, `currency CHECK (length = 3)`, `created_at`, `updated_at` |
| `transfers` | `id` PK (UUIDv7), `idempotency_key NOT NULL UNIQUE`\*, `from_wallet_id` FK, `to_wallet_id` FK, `amount CHECK (> 0)`, `status CHECK IN (PENDING, PROCESSED, FAILED)`, `failure_reason CHECK IN (known reasons)`\*, timestamps, `CHECK (from_wallet_id <> to_wallet_id)`, `CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))`\* |
| `ledger_entries` | `id` PK (UUIDv7), `transfer_id` FK, `wallet_id` FK, `entry_type CHECK IN (DEBIT, CREDIT)`, `amount CHECK (> 0)`, `created_at`, **`UNIQUE (transfer_id, entry_type)`** |
| `idempotency_records` | `"key"` PK, `request_hash NOT NULL CHECK (length = 64)`\*, `transfer_id` FK NULL `UNIQUE`\*, `created_at` |

\* Stricter than the original spec. Each extra constraint makes an invalid state
impossible in the database itself: no two transfers can share a key, a reason
exists exactly when the transfer FAILED, and a key maps to at most one transfer
(and a transfer to at most one key).

Indexes: `transfers(from_wallet_id, created_at)`, `transfers(to_wallet_id, created_at)`,
`ledger_entries(wallet_id, created_at)`.

**How the history query uses them.** `GET /wallets/:id/transfers` is written as two branches:
the newest `limit` sent and the newest `limit` received, each read backwards from its own index.
They are merged with `UNION ALL` and cut to `limit`. A single
`WHERE from_wallet_id = ? OR to_wallet_id = ?` looks simpler, but Postgres answers it by fetching
*every* transfer of the wallet (BitmapOr) and sorting them all.

Measured on Postgres 14 with 300k transfers, where one wallet appears in 60k:

| Query | Plan | Rows read for 20 results | Time |
|---|---|---|---|
| `OR` | BitmapOr → Bitmap Heap Scan → top-N sort | 60,300 | 31.8 ms |
| `UNION ALL` (current) | 2 × Index Scan Backward → Merge Append | 42 | 0.18 ms |

Both return identical rows. SQLite also searches both indexes (`EXPLAIN QUERY PLAN`). No
duplicates are possible, because `CHECK (from_wallet_id <> to_wallet_id)` keeps a transfer out of
both branches. Adding `id` to the two indexes would also remove the small tie-break sort; it is left
out to keep the schema as specified.

- `UNIQUE (transfer_id, entry_type)` makes a third ledger row impossible, so the
  database enforces "at most one DEBIT and one CREDIT per transfer".
- The `idempotency_records.key` PK makes duplicate processing impossible, even if
  application logic were wrong.
- SQLite: `STRICT` tables; timestamps are `TEXT` in a **fixed-width** UTC format
  `2006-01-02T15:04:05.000000Z` so string ordering equals time ordering
  (`RFC3339Nano` trims trailing zeros and would break ordering).
- Postgres: `UUID`, `BIGINT`, `TIMESTAMPTZ`.
- `"key"` is quoted in SQL because `KEY` is a keyword in both databases.
- Migrations are embedded `*.sql` files, applied in order by a small runner
  (`sqlrepo.Migrate`). Each file runs in its own transaction, recorded in
  `schema_migrations`. The runner assumes a single migrating process; several
  replicas would need an advisory lock.
- Seed migration `0002_demo_seed.sql` (demo only): `wallet_1`=100000,
  `wallet_2`=50000, `wallet_3`=0 (SGD), `wallet_usd`=10000 (USD). It is applied
  only when `repository.Options.SeedDemoData` is set: the server sets it, the
  tests do not, and they seed their own wallets.
- Timestamps are bound as the fixed-width string on both drivers. Postgres
  parses it into `TIMESTAMPTZ`. One scanner reads either `time.Time` (Postgres)
  or `TEXT` (SQLite) back into UTC.

## 5. Idempotency

- **Storage:** `idempotency_records(key, request_hash, transfer_id)`, written in
  **the same transaction** as the transfer, balances and ledger.
- **Request hash:** SHA-256 hex over a **length-prefixed** encoding of
  `(fromWalletId, toWalletId, amount)`. This canonical form ignores JSON
  whitespace and field order. Length prefixes stop `"a|b","c"` from colliding
  with `"a","b|c"`.
- **Detection:** `INSERT … ON CONFLICT (key) DO NOTHING`. Zero rows affected means
  the key exists:
  - same hash → load the linked transfer, return it with `Replayed = true`;
  - different hash → `ErrIdempotencyKeyReused` (409).
- **Concurrent duplicates:** the second insert on the same key waits on the
  unique index (Postgres) or behind the write lock (SQLite). If the first
  commits, the second sees the row and replays. If the first rolls back, the
  second proceeds as a new request.
- **What is stored:** PROCESSED and FAILED outcomes (they are final business
  results). **Not stored:** 404, validation errors, 503, 500. Nothing commits in
  those cases, so a retry with the same key runs again.
- **Lost response:** if the transaction committed but the response was lost, the
  client's retry replays the stored result. The idempotency row is durable, so
  this holds across restarts.

## 6. Transaction boundary and algorithm

One database transaction per request covers everything:

1. Validate the command (defense in depth; the handler validates too).
2. Compute `requestHash`.
3. `WithinTx`:
   1. Insert the idempotency record (`transfer_id` NULL); on conflict → replay or 409.
   2. **Lock and load both wallets in sorted-id order.** Missing → `ErrWalletNotFound`,
      which rolls everything back, including the idempotency row.
   3. Insert the transfer as `PENDING`.
   4. Business rules via `domain.ApplyTransfer(from, to, amount)`: currency, then
      funds, then destination overflow. All rules are checked before either
      wallet changes. On a rule failure:
      `MarkFailed(reason)`, guarded status update, link the idempotency record, commit.
   5. Update both balances; insert DEBIT(from) and CREDIT(to).
   6. `MarkProcessed()`; guarded update
      `UPDATE transfers SET status=?, failure_reason=?, updated_at=? WHERE id=? AND status='PENDING'`
      (exactly 1 row, else `ErrInvalidTransition`).
   7. Link the idempotency record; commit.
4. Log once, after commit. There is no I/O inside the transaction other than SQL.

> **Deviation from the original spec:** it inserted the transfer (3.3) *before*
> loading the wallets (3.2). Foreign keys are checked immediately on insert, so a
> missing wallet would surface as an FK violation instead of `ErrWalletNotFound`.
> Locking the wallets first fixes this. It also takes the locks before any other
> write.

## 7. Concurrency

| | PostgreSQL | SQLite |
|---|---|---|
| Isolation | `READ COMMITTED` | serializable by construction (single writer) |
| Wallet locking | `SELECT … FOR UPDATE` on both wallets, **sorted by id** (no lock-order deadlocks, e.g. A→B racing B→A) | `BEGIN IMMEDIATE` (`_txlock=immediate`) takes the write lock at tx start |
| Waiting | `set_config('lock_timeout', …, true)` (= `SET LOCAL`) | `busy_timeout = LOCK_TIMEOUT` |
| Pool | 20 connections, shared by reads and writes | **1 writer connection** + 4 read connections (see below) |
| Last line of defense | `CHECK (balance >= 0)` | same |

Why pessimistic row locks rather than optimistic versioning: transfers on a hot
wallet contend by nature. Row locks queue the requests instead of making them
fail and retry. The lock set is small (two rows) and has a fixed order.

Why `READ COMMITTED` and not `SERIALIZABLE`: the rows we decide on are locked
explicitly, so SERIALIZABLE would add serialization failures without adding
safety.

Why the idempotency insert comes first: a concurrent duplicate blocks on the key
before it touches any wallet, so it never holds wallet locks while waiting.

**Guarded balance write (defense in depth).** The balance update is a compare-and-set,
`UPDATE wallets SET balance = ? … WHERE id = ? AND balance = <value read under the lock>`.
With the lock held it always matches. If a future code path forgot to lock, the write would fail
with `ErrStaleWrite` (transaction rolled back, 500) instead of silently losing a concurrent
update, which `CHECK (balance >= 0)` would not catch.

**Evidence (red → green):** with `FOR UPDATE` temporarily removed, the 50-way
double-spend test on Postgres let **all 50** transfers of 10 "succeed" from a
balance of 100. Lost updates created money. With the lock: exactly 10
PROCESSED, 40 FAILED, final balance 0. After the guarded write was added, the same no-lock
experiment no longer creates money: the conflicting write is rejected (`row changed since it was
read`), and the invariant checker reports no violation.

**Why SQLite writes use a single connection:** the first run of the
500-transfer stress test failed with `SQLITE_BUSY` after 5s. With an unbounded
pool, 500 connections busy-polled for the one write lock, and SQLite's busy
handler is not a fair queue, so one waiter starved past `busy_timeout`. Capping
the pool at 4 made it rarer, but repeated runs under load still failed. The fix
is the standard SQLite pattern: a dedicated **one-connection writer pool**.
Write transactions queue in `database/sql`, bounded by the request deadline,
and never fight each other inside SQLite. Reads use a separate 4-connection
pool and run concurrently with the writer (WAL). `busy_timeout` still covers
contention from outside the writer pool. The fix held across 5 repeated runs of
the full suite under the race detector.

### Retry policy (TxManager)

The **whole transaction** is retried, up to 3 attempts with jittered backoff.
This is safe because the idempotency insert is part of the transaction being
retried.

| Error | Retry? | Result |
|---|---|---|
| Postgres `40P01` deadlock, `40001` serialization failure | yes | 503 after 3 attempts |
| SQLite `SQLITE_BUSY_SNAPSHOT` (stale WAL snapshot; waiting cannot help) and `SQLITE_LOCKED` | yes | 503 after 3 attempts |
| Postgres `55P03` lock_timeout, SQLite BUSY after `busy_timeout` | **no** — already waited `LOCK_TIMEOUT`; retrying would outlive `REQUEST_TIMEOUT` | 503 |
| `context.DeadlineExceeded` / `Canceled` | no | 503 |
| anything else | no | propagated (e.g. 500) |

Classification lives in `Dialect.IsRetryable` / `Dialect.IsLockTimeout` /
`Dialect.IsUniqueViolation`. `IsLockTimeout` is an addition to the original
Dialect interface; the no-retry-on-lock-timeout rule needs it.

Infrastructure failures that should become 503 are wrapped in
`service.ErrUnavailable`. This is a service-level sentinel, not a domain one,
because it describes infrastructure.

## 8. State machine

```
PENDING ──MarkProcessed()──▶ PROCESSED
   │
   └────MarkFailed(reason)──▶ FAILED
```

- The domain methods reject every transition that does not start at PENDING
  (`ErrInvalidTransition`) and leave state unchanged.
- The database update is **guarded** (`WHERE status = 'PENDING'`), so a stale or
  duplicate writer cannot move a final transfer.
- Everything happens in one transaction, so no other transaction ever observes
  PENDING. The state is still modeled and enforced because it is the natural
  extension point for a two-phase (async) design (§12).

## 9. Failure modes

| Scenario | Behavior |
|---|---|
| Duplicate request (sequential) | replay, same body and status, `Idempotent-Replayed: true` |
| Duplicate request (concurrent) | one processes; others wait, then replay |
| Same key, different payload | 409, nothing written |
| Wallet missing | 404, rollback; nothing stored; key reusable |
| Insufficient funds / currency mismatch / overflow | 422, FAILED transfer stored, no ledger rows, balances unchanged |
| Crash / cancel before commit | rollback; nothing stored; retry with same key succeeds |
| Commit succeeded, response lost | retry replays stored result |
| Deadlock / serialization failure | whole tx retried (≤3); then 503 |
| Lock wait exceeds `LOCK_TIMEOUT` | 503, no retry |
| Request exceeds `REQUEST_TIMEOUT` | context cancelled, rollback, 503 |
| Database down | `/readyz` 503; writes 503/500 with JSON body |
| Panic | recovered, 500 JSON, server keeps serving |
| Bug attempts negative balance / 3rd ledger row / invalid status | rejected by DB constraints |

## 10. Observability

- Middleware order: RequestID → Logger → Recovery → Timeout → BodyLimit.
- One JSON (`log/slog`) line per request: `request_id, method, route, status,
  duration_ms, idempotency_key, transfer_id, outcome` (`processed|failed|replayed|error`).
- Request bodies are never logged.
- The log line carries the internal error text in an `error` field. Clients only
  ever see the fixed, safe message.
- Panics are logged with their stack and returned as a 500 `INTERNAL_ERROR`.
- An incoming `X-Request-ID` is echoed only if it matches `[A-Za-z0-9._:-]{1,128}`;
  otherwise a UUID replaces it (prevents header and log injection).
- The DSN is never logged (it may contain a password).
- Graceful shutdown on SIGINT/SIGTERM: stop accepting connections and give
  in-flight requests 10s. A request cut off by the timeout rolls back, because
  it is a single transaction. Verified with a real SIGTERM.

## 11. Testing strategy

- **Domain unit tests:** debit/credit edges, full state transition matrix,
  command validation, hash determinism.
- **Handler tests:** `httptest` + mocked service; every 400 case, 413, error
  mapping, replay header, request ID, panic recovery, no leaked internals.
- **Integration tests** (`tests/integration`): real database, table-driven over
  drivers. SQLite always (file in `t.TempDir()`); Postgres when
  `TEST_POSTGRES_DSN` is set, each test in its own randomly named schema. Each
  test seeds its own wallets.
- **Invariant checker** after every integration test: total balance conserved;
  each PROCESSED transfer has exactly one DEBIT(from) + one CREDIT(to) of the
  amount; FAILED/PENDING have none; no PENDING left; per-wallet
  `balance == opening + credits − debits`; every idempotency record is linked.
- **Concurrency tests** use goroutines released by a start barrier so requests
  truly overlap. The Postgres double-spend test is shown failing once with
  `FOR UPDATE` removed (red), then passing.
- **Constraint tests** bypass the service and prove the database rejects
  invalid data on its own.
- **End-to-end:** the real router over a real database (both drivers). With
  `TEST_POSTGRES_DSN` set, `cmd/server` also boots the real binary with
  `DB_DRIVER=postgres` and drives every outcome over HTTP. `make verify-pg` adds
  the demo and SQL invariant checks against a live server (see
  [postgres.md](postgres.md)). Also a
  real server boot → serve → graceful shutdown test in `cmd/server`, and a
  closed-database test (`/readyz` 503, POST returns a JSON error with no driver
  text).
- The invariant checker itself was verified once with a throwaway test that
  broke every invariant on purpose; each violation was reported.

## 12. Assumptions and tradeoffs

- Wallets are pre-provisioned; there is no creation API. A currency never changes.
- Idempotency keys are global, not scoped per client (no auth in scope).
- Replays reconstruct the response from the transfer row instead of storing
  response bytes. The response is a pure function of the transfer, so this is
  equivalent and keeps one source of truth.
- Stored balance plus ledger (instead of deriving balance from the ledger): O(1)
  reads and a simple lock target. The invariant checker and a future
  reconciliation job verify the two agree.
- SQLite serializes all writers. That is correct but limits throughput; Postgres
  is the production target.
- **Ledger balance is enforced in code and verified by tests, not fully by the
  database.** The schema guarantees *at most* one DEBIT and one CREDIT per
  transfer (`UNIQUE (transfer_id, entry_type)`), positive amounts, and valid
  foreign keys. It does not guarantee that both lines exist, that their amounts
  equal the transfer amount, or that they are on the from/to wallets. That would
  need Postgres-only deferred constraint triggers, which SQLite cannot mirror.
  Instead, `domain.NewLedgerEntries` only ever builds the matching pair, and the
  invariant checker verifies the full rule after every integration test. A
  reconciliation job (§13) would verify it in production.

## 13. Future work

- **Transactional outbox** for emitting `TransferProcessed` events.
- **Two-phase / async processing** with a recovery worker that resolves stale PENDING transfers.
- **Idempotency key TTL** and cleanup job.
- **Reconciliation job** comparing balances with ledger sums.
- **Per-client key scoping** (`(client_id, key)` unique) once authentication exists.
- Metrics (latency histograms, outcome counters, lock wait time).
