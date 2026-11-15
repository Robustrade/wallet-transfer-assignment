# Wallet Transfer Service — Design Note

Written before implementation and kept up to date as the implementation landed.

## 1. Problem statement

Move money between two wallets in a way that stays correct when clients retry, requests arrive twice, many transfers hit the same wallet at the same time, or the process crashes partway through.

Guarantees:

- **Exactly-once at the API level.** One `idempotencyKey` produces at most one transfer, and every retry gets the original result.
- **Atomicity.** A transfer's balance changes, ledger entries, status and idempotency record all commit together or not at all.
- **No double spending.** A wallet balance never goes below zero, including under concurrent debits.
- **Balanced ledger.** Every processed transfer has exactly one DEBIT and one CREDIT for the same amount.

Out of scope: funding wallets beyond an opening balance (wallets are created through `POST /create-wallet` with an initial balance), multiple currencies, authentication, and async or external payment rails.

## 2. Technology

| Concern | Choice | Reason |
|---|---|---|
| Language | Go | The CI pipeline targets Go (`setup-go`, `golangci-lint`) |
| Database | PostgreSQL 16 | Preferred by the spec. Row-level locks and `ON CONFLICT` give the guarantees we need |
| Data access | GORM (pgx/v5 underneath) | Concise queries with explicit support for transactions, `FOR UPDATE` and `ON CONFLICT`; GORM models stay in the repository layer |
| HTTP | Gin | Widely used; request binding keeps controllers thin; middleware for request IDs and logging |
| Migrations | golang-migrate, versioned SQL files embedded with `go:embed`, applied at startup | Schema is explicit and reviewable; each change runs exactly once and is recorded in `schema_migrations` |
| Configuration | environment variables, loaded from `.env` via godotenv | No settings or credentials in code; `.env.example` documents every variable |
| Logging | `log/slog` (JSON) | Structured, standard library |
| Tests | `testing` + testcontainers-go | Concurrency and locking can only be proven against real Postgres |

## 3. API contract

All amounts are **positive integers in minor units** (e.g. cents). Fractional or non-numeric amounts are rejected. Every response carries an `X-Request-ID` header (the client's value if it sent one, otherwise a generated UUID).

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

| Outcome | Status | Body |
|---|---|---|
| Transfer processed | `201 Created` | transfer (`status: "PROCESSED"`) |
| Insufficient funds | `422 Unprocessable Entity` | error `INSUFFICIENT_FUNDS` + transfer (`status: "FAILED"`) |
| Invalid request (missing fields, `amount <= 0`, same wallet, blank or too-long key) | `400 Bad Request` | error `VALIDATION_ERROR` with the offending `field` |
| Wallet does not exist | `404 Not Found` | error `WALLET_NOT_FOUND` |
| Key reused with a different payload | `409 Conflict` | error `IDEMPOTENCY_KEY_REUSED` |
| Wallet locked for longer than the lock timeout | `503 Service Unavailable` | error `SERVICE_BUSY` (nothing persisted, safe to retry) |
| Unexpected or database failure | `500 Internal Server Error` | error `INTERNAL` (nothing persisted, safe to retry) |

A retry with the same key and payload gets the **same status code and body** as the original, plus the header `Idempotent-Replayed: true`.

Transfer body:

```json
{
  "transferId": "0b9e…",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-11-10T10:00:00.123456Z"
}
```

Error body: `{"error": {"code": "INSUFFICIENT_FUNDS", "message": "…"}}`, with a `field` for validation errors and a `transfer` field when a FAILED transfer was recorded.

### `POST /create-wallet`

```json
{ "walletId": "wallet_1", "initialBalance": 2000 }
```

| Outcome | Status | Body |
|---|---|---|
| Wallet created | `201 Created` | `{ "walletId", "balance", "createdAt", "updatedAt" }` |
| Missing or blank `walletId`, missing, negative or non-integer `initialBalance` | `400 Bad Request` | error `VALIDATION_ERROR` |
| Wallet ID already exists | `409 Conflict` | error `WALLET_ALREADY_EXISTS` |

The client chooses the wallet ID. `initialBalance` is an opening balance and does not create ledger entries (see section 11).

### `GET /healthz`

Returns `200 {"status":"ok"}` when the service is up and the database is reachable, `503` otherwise.

## 4. Decisions on points the spec leaves open

| Question | Decision | Reasoning from the spec |
|---|---|---|
| Is `idempotencyKey` required? | **Required** (400 if missing) | The spec only defines exactly-once "when an idempotencyKey is provided", yet also requires correct behaviour "under retries and duplicate delivery". A transfer without a key cannot be retried safely, so it is not accepted. |
| What happens on insufficient funds? | Transfer recorded as **FAILED**; retries return that same failure | Uses the `PENDING -> FAILED` lifecycle from the spec. A retry returns the original failure instead of silently succeeding later, which matches "return the original result". |
| What happens when a wallet doesn't exist? | **404, nothing persisted** | Treated as request validation that fails before execution starts, like a 400. Industry practice (e.g. Stripe) is to save idempotent results only once execution begins. It also keeps foreign keys strict: no transfer can reference a missing wallet. |
| Same key, different payload? | **409**, original untouched | A key identifies one intent. Reusing it for a different transfer is a client bug and must not be hidden. |
| Stored or derived balance? | **Stored balance** on `wallets`, updated in the same transaction as the ledger | Reading a balance is O(1), and row locks on the wallet give a natural serialization point. The ledger stays the audit trail and can be reconciled against balances. |
| Store the raw response, or rebuild it on replay? | **Rebuild** from the stored transfer row | The transfer row already holds every field of the response, so the replay is identical without duplicating data, and the service stays free of HTTP concerns. |

## 5. Schema

Summary below; the authoritative version, with every constraint explicitly named, is [`migrations/000001_create_tables.up.sql`](../migrations/000001_create_tables.up.sql).

```sql
CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT      NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_records (
    key            TEXT PRIMARY KEY,
    request_hash   TEXT        NOT NULL,          -- SHA-256 of (from, to, amount)
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id               UUID PRIMARY KEY,
    idempotency_key  TEXT   NOT NULL UNIQUE REFERENCES idempotency_records(key),
    from_wallet_id   TEXT   NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT   NOT NULL REFERENCES wallets(id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    status           TEXT   NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

CREATE TABLE ledger_entries (
    id           BIGSERIAL PRIMARY KEY,
    transfer_id  UUID   NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT   NOT NULL REFERENCES wallets(id),
    entry_type   TEXT   NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_entries_wallet  ON ledger_entries (wallet_id, created_at);
CREATE INDEX idx_transfers_from_wallet  ON transfers (from_wallet_id, created_at);
CREATE INDEX idx_transfers_to_wallet    ON transfers (to_wallet_id, created_at);
```

What the database enforces on its own, even if application code has a bug:

| Invariant | Enforced by |
|---|---|
| No negative balance / double spend | `CHECK (balance >= 0)` |
| At most one transfer per idempotency key | `UNIQUE (idempotency_key)` |
| No ledger row without a transfer or wallet | foreign keys |
| At most one DEBIT and one CREDIT per transfer | `UNIQUE (transfer_id, entry_type)` |
| No self-transfer, no zero or negative amount | `CHECK` constraints |
| Only valid statuses; a failure reason exactly when FAILED | `CHECK` constraints |

Money is `BIGINT` minor units, never floating point. Transfer IDs are UUIDs generated by the service, so they are not guessable and are known before the insert.

## 6. Transfer workflow

The request is validated by the domain first (bad input never opens a transaction). Everything else runs in **one database transaction** at `READ COMMITTED`:

```text
BEGIN
 0. SET LOCAL lock_timeout = '5s'
 1. INSERT INTO idempotency_records (key, request_hash)
      ON CONFLICT (key) DO NOTHING
    -> 0 rows inserted: key already used
         hash differs  -> ROLLBACK, 409
         hash matches  -> load the transfer by idempotency key, COMMIT (nothing written), replay it
 2. SELECT * FROM wallets
      WHERE id IN (from, to) ORDER BY id FOR UPDATE
    -> fewer than 2 rows -> ROLLBACK, 404 (key not consumed)
 3. INSERT INTO transfers (..., status = 'PENDING')
 4. if from.balance < amount:
        UPDATE transfers SET status = 'FAILED', failure_reason = 'INSUFFICIENT_FUNDS'
          WHERE id = $1 AND status = 'PENDING'
    else:
        UPDATE wallets SET balance = <new balance> WHERE id = from
        UPDATE wallets SET balance = <new balance> WHERE id = to
        INSERT ledger_entries (DEBIT from), (CREDIT to)
        UPDATE transfers SET status = 'PROCESSED'
          WHERE id = $1 AND status = 'PENDING'
COMMIT
```

New balances are computed by the domain (`Wallet.Debit` / `Wallet.Credit`) from the values read under the row lock, so no other transaction can change them in between.

### 6.1 Idempotency

- **Storage.** The key is the primary key of `idempotency_records`, stored with a SHA-256 fingerprint of the payload. It lives in Postgres, so it survives restarts and works across multiple instances.
- **Detecting duplicates.** `INSERT … ON CONFLICT DO NOTHING` on the primary key is atomic. There is no check-then-insert race.
- **Concurrent duplicates.** If request B with key K arrives while request A (same K) is still running, B's insert **waits on the unique index** until A finishes. If A commits, B sees the record and replays A's transfer. If A rolls back, B's insert succeeds and B executes normally. Exactly one of them executes.
- **Returning the original result.** The transfer is loaded by its idempotency key and the response is rebuilt from it. Timestamps are stored and returned in UTC at microsecond precision, so the replayed body is byte-for-byte identical to the original.
- **No duplicate side effects.** The record is written in the same transaction as the balance changes, so "key consumed" and "money moved" are never out of sync. In addition, `transfers.idempotency_key UNIQUE` makes a second transfer for one key impossible at the database level.
- **Lost response.** If the transaction commits but the response never reaches the client, the client retries with the same key and receives the original result. Nothing runs twice.
- **No stuck "in progress" state.** Because there is a single transaction, a crash or error mid-way rolls back everything, including the idempotency record. The client's retry then runs from scratch.

### 6.2 Concurrency

**Strategy: pessimistic row locks (`SELECT … FOR UPDATE`) on both wallets, acquired in a deterministic order (sorted by wallet id).**

- Two transfers debiting the same wallet are serialized on that wallet's row lock. The second reads the balance only after the first commits, so there is no read-then-write race and no double spending.
- Locking in sorted order means `A→B` and `B→A` running at the same time both lock `A` first, so they cannot deadlock.
- `lock_timeout` bounds how long a request waits for a lock; if it is exceeded (or Postgres reports a deadlock), the transaction rolls back and the client gets a retryable `503`.
- The `CHECK (balance >= 0)` constraint is a second line of defence: even a buggy code path cannot commit an overdraft.

Alternatives considered:

| Option | Why not chosen |
|---|---|
| Optimistic locking (version column + retry) | Popular wallets would conflict constantly and need retry loops; wasted work under contention |
| `SERIALIZABLE` isolation | Correct, but every conflict aborts with a serialization failure (`40001`) and needs an application retry loop; harder to reason about |
| Single conditional update (`UPDATE … SET balance = balance - x WHERE balance >= x`) | Also correct for the debit, but explicit locking keeps the "check funds, then record FAILED or PROCESSED" logic straightforward |

Trade-off: transfers touching the same wallet are serialized, which caps throughput per wallet. This is acceptable here and is the standard trade-off for strongly consistent balances.

### 6.3 State machine

```text
PENDING ──> PROCESSED
   └──────> FAILED
```

- The domain model allows only `PENDING → PROCESSED` and `PENDING → FAILED`. Every other transition returns an error.
- The SQL update repeats that guard (`WHERE status = 'PENDING'`), and the repository treats "0 rows updated" as an error. A late or duplicate update can therefore never overwrite a final state.
- PENDING exists only inside the transaction today. Keeping it in the model means an async flow (e.g. an external payment rail) could later commit PENDING first and finalize it in a separate step, without a schema change.

## 7. Failure modes

| Failure | Behaviour |
|---|---|
| Duplicate request after success | Original transfer replayed, no new side effects |
| Duplicate request while the original is running | Waits on the key, then replays (or executes if the original rolled back) |
| Process crash / DB error before commit | Full rollback; key not consumed; retry runs from scratch |
| Commit succeeded, response lost | Retry with the same key returns the original result |
| Insufficient funds | FAILED transfer recorded, 422; retries get the same 422 |
| Unknown wallet | 404, nothing stored; retry re-evaluates |
| Lock wait too long | `lock_timeout` aborts the transaction, 503, safe to retry |
| Deadlock | Prevented by ordered locking; if Postgres still reports one, it rolls back and returns 503 (retryable) |
| Shutdown during a request | SIGTERM/SIGINT stops new requests, in-flight requests get up to 3 s to finish, then the DB pool is closed |

## 8. Architecture

```text
cmd/server/            main: config, migrations, DB connection, middleware, routes, HTTP server, graceful shutdown
internal/config/       settings from environment variables / .env
internal/database/     shared GORM connection (database.DB) and migration runner
internal/router/       every route (URL -> controller function)
internal/middleware/   request ID + structured request logging
internal/controller/   bind JSON, check request shape, call the service, respond
internal/response/     shared success/error responses; domain error -> HTTP status mapping
internal/service/      business logic: wallet creation, transfer workflow, idempotency, transactions
internal/repository/   Repository interface, Postgres implementation (repository.Repo), GORM models
internal/domain/       Transfer, Wallet, LedgerEntry, state transitions, validation, domain errors
internal/testutil/     throwaway Postgres for tests (testcontainers)
migrations/            versioned SQL schema (embedded)
```

- **Handler layer** (router + controller + response). Routes requests, parses JSON, checks request shape, calls the service and writes the response. No business rules. The spec's "handler" is split into these three small packages so that every URL is visible in one file and error-to-status mapping lives in one place.
- **Service.** Owns the transaction boundary (`database.DB.Transaction(func(tx *gorm.DB) error { … })`), idempotency and the workflow above.
- **Repository.** A `Repository` interface with one focused method per query, implemented for Postgres and exposed as `repository.Repo`. Every method takes the `*gorm.DB` to run on as its first argument, so inside a transaction the service passes `tx` and all statements share it. Postgres errors are translated to domain errors here (unique violation → already exists, lock timeout/deadlock → service busy). No workflow decisions.
- **Domain.** Pure Go with no database or HTTP imports, so it is unit-testable in isolation.

## 9. Observability and operations

- **Structured JSON logs** (`log/slog`). Every request logs `request_id`, method, path, status, latency and client IP. Every transfer logs `request_id`, `transfer_id`, `idempotency_key`, final `status` and whether it was `replayed`.
- **Request IDs.** Taken from `X-Request-ID` or generated, returned in the response header and attached to every log line, so one request can be traced end to end.
- **Internal errors** are logged with the request ID and the underlying error; the client only sees a generic message.
- **Health check:** `GET /healthz` pings the database.
- **HTTP server timeouts** (read header 5 s, read 10 s, write 15 s, idle 60 s) and **graceful shutdown** on SIGTERM/SIGINT.
- A natural next step (not implemented) is metrics: a transfer counter by outcome and a latency histogram.

## 10. Testing strategy

Red → green: tests for each behaviour were written before the code that satisfies it (the commit history shows tests landing first).

- **Domain unit tests** (`internal/domain`): request validation (including blank values), allowed and forbidden state transitions, ledger entry generation, wallet debit/credit rules.
- **API tests** (`internal/controller`, real HTTP through the router against real Postgres):
  - wallet creation: success, zero balance, duplicate ID (409, original untouched), invalid input (nothing saved)
  - successful transfer moves balances and writes exactly one DEBIT and one CREDIT
  - insufficient funds: 422, FAILED transfer recorded, balances unchanged, no ledger entries
  - retry with the same key: identical body, `Idempotent-Replayed: true`, money moved once
  - retry of a failed transfer: same 422
  - same key, different payload: 409, no change
  - unknown wallet: 404, key not consumed, the same key works once the wallet exists
  - invalid input: 400 naming the field, nothing saved
- **Concurrency tests** (`internal/service`, 20 goroutines released at the same instant):
  - 20 debits of 100 from a wallet holding 1000: exactly 10 succeed, final balance 0, ledger balanced
  - 20 requests with the same key: exactly one executes, all receive the same transfer
  - concurrent `A→B` and `B→A` transfers: all complete, no deadlock, total money conserved
  - Verified to be meaningful: with `FOR UPDATE` removed, the first test fails (all 20 "succeed", balances suffer lost updates, ledger and balances disagree).

All tests run with `-race`. Each test package starts its own throwaway Postgres via testcontainers; if Docker is unavailable the database tests are skipped with a clear message.

## 11. Assumptions and trade-offs

- Single currency; amounts are integer minor units.
- Wallets are created through `POST /create-wallet` with a client-chosen ID and an opening balance, which is not recorded in the ledger. This is for setup and testing. In production, funding would be a ledger-backed deposit from a funding or treasury account, so that `balance = opening + Σcredits − Σdebits` reconciles exactly per wallet.
- The database connection is a package-level `database.DB`, and controllers create services directly. Transactional correctness does not depend on that global: every repository method receives the `*gorm.DB` (or `tx`) explicitly. The repository interface (`repository.Repo`) is the seam for substituting a fake in tests if needed.
- Replays rebuild the response from the transfer row; if the response format changed later, a replay would use the new format. Storing raw responses would avoid that at the cost of duplicated data.
- Migrations run at application startup for simplicity; production would typically run them as a separate deploy step.
- Idempotency records are kept indefinitely; production would add a retention window.
- Throughput per wallet is limited by row-lock serialization (see 6.2).
