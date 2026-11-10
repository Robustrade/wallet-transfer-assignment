# Wallet Transfer Service — Design Note

Status: draft, written before implementation. It is updated as the implementation lands.

## 1. Problem statement

Move money between two wallets in a way that stays correct when clients retry, requests arrive twice, many transfers hit the same wallet at the same time, or the process crashes partway through.

Guarantees:

- **Exactly-once at the API level.** One `idempotencyKey` produces at most one transfer, and every retry gets the original result.
- **Atomicity.** A transfer's balance changes, ledger entries, status and idempotency record all commit together or not at all.
- **No double spending.** A wallet balance never goes below zero, including under concurrent debits.
- **Balanced ledger.** Every processed transfer has exactly one DEBIT and one CREDIT for the same amount.

Out of scope: creating or funding wallets (they are seeded), multiple currencies, authentication, and async or external payment rails.

## 2. Technology

| Concern | Choice | Reason |
|---|---|---|
| Language | Go | The CI pipeline targets Go (`setup-go`, `golangci-lint`) |
| Database | PostgreSQL 16 | Preferred by the spec. Row-level locks and `ON CONFLICT` give the guarantees we need |
| Driver | `pgx/v5` (pool) | Native Postgres driver with typed error codes |
| HTTP | standard library `net/http` | Go 1.22+ routing patterns are enough; a framework adds nothing here |
| Migrations | embedded SQL, applied at startup | One fewer moving part |
| Logging | `log/slog` (JSON) | Structured, standard library |
| Tests | `testing` + testcontainers-go | Concurrency and locking can only be proven against real Postgres |

## 3. API contract

All amounts are **positive integers in minor units** (e.g. cents). Fractional or non-numeric amounts are rejected.

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
| Invalid request (missing fields, `amount <= 0`, same wallet, bad key) | `400 Bad Request` | error `VALIDATION_ERROR` |
| Wallet does not exist | `404 Not Found` | error `WALLET_NOT_FOUND` |
| Key reused with a different payload | `409 Conflict` | error `IDEMPOTENCY_KEY_REUSED` |
| Unexpected or database failure | `500` / `503` | error `INTERNAL` (nothing persisted, safe to retry) |

A replayed response has the **same status code and body** as the original, plus the header `Idempotent-Replayed: true`.

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
  "createdAt": "2026-11-10T10:00:00Z"
}
```

Error body: `{"error": {"code": "INSUFFICIENT_FUNDS", "message": "…"}}`, with a `transfer` field added when a FAILED transfer was recorded.

### Read endpoints (optional enhancements)

- `GET /transfers/{id}` returns a transfer
- `GET /wallets/{id}` returns `{ "walletId", "balance", "updatedAt" }`
- `GET /healthz` is a liveness check

## 4. Decisions on points the spec leaves open

| Question | Decision | Reasoning from the spec |
|---|---|---|
| Is `idempotencyKey` required? | **Required** (400 if missing) | The spec only defines exactly-once "when an idempotencyKey is provided", yet also requires correct behaviour "under retries and duplicate delivery". A transfer without a key cannot be retried safely, so it is not accepted. |
| What happens on insufficient funds? | Transfer recorded as **FAILED** and the outcome stored for replay | Uses the `PENDING -> FAILED` lifecycle from the spec. A retry returns the original failure instead of silently succeeding later, which matches "return the original result". |
| What happens when a wallet doesn't exist? | **404, nothing persisted** | Treated as request validation that fails before execution starts, like a 400. Industry practice (e.g. Stripe) is to save idempotent results only once execution begins. It also keeps foreign keys strict: no transfer can reference a missing wallet. |
| Same key, different payload? | **409**, original untouched | A key identifies one intent. Reusing it for a different transfer is a client bug and must not be hidden. |
| Stored or derived balance? | **Stored balance** on `wallets`, updated in the same transaction as the ledger | Reading a balance is O(1), and row locks on the wallet give a natural serialization point. The ledger stays the audit trail and can be reconciled against balances. |

## 5. Schema

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
    response_code  INT,                           -- set before commit
    response_body  JSONB,
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

Money is `BIGINT` minor units, never floating point.

## 6. Transfer workflow

Everything below runs in **one database transaction** at `READ COMMITTED`, with `SET LOCAL lock_timeout` so a stuck lock fails fast instead of hanging the request.

```text
BEGIN
 1. INSERT INTO idempotency_records (key, request_hash)
      ON CONFLICT (key) DO NOTHING
    -> 0 rows inserted: key already used
         load record; hash differs  -> ROLLBACK, 409
                      hash matches  -> ROLLBACK, replay stored response
 2. SELECT id, balance FROM wallets
      WHERE id IN (from, to) ORDER BY id FOR UPDATE
    -> fewer than 2 rows            -> ROLLBACK, 404 (key not consumed)
 3. INSERT INTO transfers (..., status = 'PENDING')
 4. if from.balance < amount:
        UPDATE transfers SET status = 'FAILED', failure_reason = 'INSUFFICIENT_FUNDS'
          WHERE id = $1 AND status = 'PENDING'
    else:
        UPDATE wallets SET balance = balance - amount WHERE id = from
        UPDATE wallets SET balance = balance + amount WHERE id = to
        INSERT ledger_entries (DEBIT from), (CREDIT to)
        UPDATE transfers SET status = 'PROCESSED'
          WHERE id = $1 AND status = 'PENDING'
 5. UPDATE idempotency_records SET response_code, response_body
COMMIT
```

### 6.1 Idempotency

- **Storage.** The key is the primary key of `idempotency_records`, stored with a hash of the payload and the exact response sent. It lives in Postgres, so it survives restarts and works across multiple instances.
- **Detecting duplicates.** `INSERT … ON CONFLICT DO NOTHING` on the primary key is atomic. There is no check-then-insert race.
- **Concurrent duplicates.** If request B with key K arrives while request A (same K) is still running, B's insert **waits on the unique index** until A finishes. If A commits, B sees the record and replays A's response. If A rolls back, B's insert succeeds and B executes normally. Exactly one of them executes.
- **Returning the original result.** The stored status code and body are returned unchanged.
- **No duplicate side effects.** The record is written in the same transaction as the balance changes, so "key consumed" and "money moved" are never out of sync. In addition, `transfers.idempotency_key UNIQUE` makes a second transfer for one key impossible at the database level.
- **Lost response.** If the transaction commits but the response never reaches the client, the client retries with the same key and receives the stored response. Nothing runs twice.
- **No stuck "in progress" state.** Because there is a single transaction, a crash or error mid-way rolls back everything, including the idempotency record. The client's retry then runs from scratch.

### 6.2 Concurrency

**Strategy: pessimistic row locks (`SELECT … FOR UPDATE`) on both wallets, acquired in a deterministic order (sorted by wallet id).**

- Two transfers debiting the same wallet are serialized on that wallet's row lock. The second reads the balance only after the first commits, so there is no read-then-write race and no double spending.
- Locking in sorted order means `A→B` and `B→A` running at the same time both lock `A` first, so they cannot deadlock.
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
| Duplicate request after success | Stored response replayed, no new side effects |
| Duplicate request while the original is running | Waits on the key, then replays (or executes if the original rolled back) |
| Process crash / DB error before commit | Full rollback; key not consumed; retry runs from scratch |
| Commit succeeded, response lost | Retry with the same key returns the stored response |
| Insufficient funds | FAILED transfer recorded, 422 stored and replayed |
| Unknown wallet | 404, nothing stored; retry re-evaluates |
| Lock wait too long | `lock_timeout` aborts the transaction, 503, safe to retry |
| Deadlock | Prevented by ordered locking; if Postgres still reports one, it rolls back and returns 503 (retryable) |

## 8. Architecture

```text
cmd/server/                  main: config, DB pool, migrations, HTTP server, graceful shutdown
internal/domain/             Transfer, Wallet, LedgerEntry, state transitions, validation, domain errors
internal/service/            transfer workflow, idempotency, transaction orchestration
internal/repository/postgres/ SQL for each table + transaction manager
internal/handler/            HTTP: decode/validate, call service, map errors to status codes
migrations/                  SQL schema (embedded)
```

- **Handler.** Parses JSON, checks request shape, calls the service, writes the response. No business rules.
- **Service.** Owns the transaction boundary (`txManager.WithinTx(ctx, fn)`), idempotency and the workflow above.
- **Repository.** One focused method per query. No workflow decisions.
- **Domain.** Pure Go with no database or HTTP imports, so it is unit-testable in isolation.

## 9. Observability

- Structured JSON logs with `request_id`, `idempotency_key`, `transfer_id`, outcome and latency for every transfer.
- Replays and key conflicts are logged distinctly.
- `GET /healthz` for liveness. A natural next step (not implemented) is a transfer counter by outcome and a latency histogram.

## 10. Testing strategy

Red → green: a failing test is written for each behaviour before the code that satisfies it.

- **Domain unit tests:** valid and invalid state transitions, request validation.
- **Service and repository integration tests (real Postgres via testcontainers):**
  - successful transfer moves balances, writes one DEBIT and one CREDIT, status PROCESSED
  - insufficient funds gives FAILED, balances unchanged, no ledger entries
  - same key, same payload: original result replayed, exactly one transfer
  - same key, different payload: 409, no change
  - unknown wallet: 404, key not consumed
  - ledger invariant: total DEBIT = total CREDIT; total of all balances unchanged
- **Concurrency tests:**
  - N concurrent debits from one wallet: exactly `balance / amount` succeed, final balance correct, never negative
  - N concurrent requests with the same key: exactly one transfer, all receive the same response
  - concurrent `A→B` and `B→A` transfers complete without deadlock
- **HTTP tests (`httptest`):** status codes, error bodies, `Idempotent-Replayed` header.

All tests run with `-race`. Integration tests are skipped with a clear message when Docker is unavailable.

## 11. Assumptions and trade-offs

- Single currency; amounts are integer minor units.
- Wallets are pre-seeded with an opening balance. In production, funding would itself be a ledger-backed transfer from a funding or treasury account, so that `balance = Σcredits − Σdebits` holds per wallet.
- Idempotency records are kept indefinitely; production would add a retention window.
- Throughput per wallet is limited by row-lock serialization (see 6.2).
