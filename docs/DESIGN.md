# Wallet Transfer Service: Design Note

Status: accepted · Scope: assignment submission · Author: Akhil Ryan

This note was written before the implementation. It records what the service guarantees, how it does so, and
which tradeoffs were made on purpose. The code and tests are expected to match it. Any change in behavior
updates this note first.

---

## 1. Problem statement

Move money between two wallets in a way that stays correct when clients retry, send duplicates and run
concurrently. Every successful transfer must:

- execute **atomically**: both balance changes and both ledger rows happen together or not at all
- produce **exactly two ledger entries** (one DEBIT, one CREDIT) of equal amount
- never let a wallet go **negative** (no double spend)
- run **at most once per idempotency key**: a retry returns the original result

### Non-goals

These are out of scope for this assignment and covered in the tradeoffs section (§11):

- authentication and authorization
- multi-currency
- wallet creation and funding through the API
- external payment rails
- asynchronous processing
- event publishing

## 2. API contract

All bodies are JSON. Amounts are **integers in minor units** (e.g. cents). Floats are rejected.
Error responses share one envelope:

```json
{ "error": { "code": "VALIDATION_ERROR", "message": "amount must be a positive integer" } }
```

### `POST /transfers`

Request:

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

| Field | Rule |
|---|---|
| `idempotencyKey` | required, 1–255 chars. Its scope is global (see §11) |
| `fromWalletId`, `toWalletId` | required, 1–64 chars, must differ |
| `amount` | required JSON integer, `> 0`, fits in int64 |

Unknown fields are rejected, and the body size is capped at 1 MiB.

Responses:

| Status | When | Recorded for replay? |
|---|---|---|
| `201 Created` | transfer `PROCESSED` | yes |
| `422 INSUFFICIENT_FUNDS` | transfer `FAILED` (persisted, no ledger rows); the body includes the transfer | yes |
| `422 IDEMPOTENCY_KEY_REUSED` | the key was already used with a **different** payload | n/a |
| `400 VALIDATION_ERROR` | malformed body or rule violation | no (nothing persisted) |
| `404 WALLET_NOT_FOUND` | either wallet does not exist | no (nothing persisted) |
| `503 SERVICE_BUSY` | lock wait exceeded `DB_LOCK_TIMEOUT`; safe to retry | no (rolled back) |
| `500 INTERNAL_ERROR` | unexpected failure; safe to retry | no (rolled back) |

Success body (`201`) and the `transfer` object inside a `422 INSUFFICIENT_FUNDS` body:

```json
{
  "id": "6f1c…", "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100,
  "status": "PROCESSED", "failureReason": null,
  "createdAt": "2026-11-04T10:00:00Z", "updatedAt": "2026-11-04T10:00:00Z"
}
```

A replayed response carries the header `Idempotent-Replayed: true`. Its status code and body are the
same as the original's.

### Read endpoints

- `GET /transfers/{id}` returns `200` with the transfer, or `404 TRANSFER_NOT_FOUND`
- `GET /wallets/{id}` returns `200 { "id", "balance", "updatedAt" }`, or `404 WALLET_NOT_FOUND`
- `GET /healthz` is the liveness check (the process is up)
- `GET /readyz` is the readiness check (the database is reachable)

## 3. Data model

```
wallets              (id PK, balance BIGINT CHECK >= 0, created_at, updated_at)
transfers            (id UUID PK, from_wallet_id FK, to_wallet_id FK, amount CHECK > 0,
                      CHECK from <> to, status CHECK IN (PENDING, PROCESSED, FAILED),
                      failure_reason, CHECK status/failure_reason consistency, created_at, updated_at)
ledger_entries       (id BIGINT IDENTITY PK, transfer_id FK, wallet_id FK, type CHECK IN (DEBIT, CREDIT),
                      amount CHECK > 0, created_at, UNIQUE (transfer_id, type))
idempotency_records  (key PK, request_hash, transfer_id FK DEFERRABLE INITIALLY DEFERRED NOT NULL, created_at)
schema_migrations    (version PK, applied_at)
```

The database enforces these invariants itself, rather than relying on application code:

| Invariant | Enforced by |
|---|---|
| A balance never goes negative | `CHECK (balance >= 0)`: a backstop behind the locked funds check |
| No ledger row without a transfer or wallet | foreign keys |
| At most one DEBIT and one CREDIT per transfer | `UNIQUE (transfer_id, type)` |
| A key maps to exactly one transfer | `PRIMARY KEY (key)` + `transfer_id NOT NULL` |
| A FAILED transfer has a reason; others do not | `CHECK` on `(status, failure_reason)` |
| No self-transfers and no non-positive amounts | `CHECK` constraints |

Indexes: `transfers(from_wallet_id, created_at)`, `transfers(to_wallet_id, created_at)` and
`ledger_entries(wallet_id, created_at)` support history queries and foreign-key lookups.
`ledger_entries(transfer_id)` is covered by the unique index.

**Balance model:** the balance is stored on the wallet and updated in the same transaction as the ledger
rows. Reads are O(1), and the ledger is the audit trail. Tests assert that each wallet's balance change
equals its credits minus its debits.

## 4. Transfer workflow

The whole workflow runs in **one database transaction** at `READ COMMITTED`, with `lock_timeout` set:

```
BEGIN
 1. INSERT INTO idempotency_records (key, request_hash, transfer_id=<new uuid>) ON CONFLICT DO NOTHING
      0 rows → SELECT the existing record
               hash differs → IDEMPOTENCY_KEY_REUSED
               hash equal   → load its transfer, return it as a replay   (ROLLBACK: nothing to write)
 2. SELECT … FROM wallets WHERE id = $1 FOR UPDATE   -- for each wallet, in ascending id order
      missing → WALLET_NOT_FOUND (ROLLBACK removes the claim from step 1)
 3. INSERT transfer (status = PENDING)
 4. if source.balance < amount → transfer.Fail(INSUFFICIENT_FUNDS)
    else → UPDATE balances (−amount / +amount), INSERT DEBIT + CREDIT ledger rows, transfer.Process()
    UPDATE transfers SET status = … WHERE id = $1 AND status = 'PENDING'   -- must affect exactly 1 row
COMMIT
```

The idempotency claim, the transfer, the ledger rows and the balance changes commit together. As a result,
a committed idempotency record **always** points at a transfer in a terminal state. No "in progress"
record is ever visible to other transactions.

**Atomicity is tested, not assumed.** [`atomicity_test.go`](../internal/service/atomicity_test.go) wraps
the real store, lets each write happen, then simulates a crash right after it. It covers the key claim,
the transfer insert, the debit, the credit, the ledger pair and the final status update, on both the
processed and the failed path, plus a panic. Every case must leave no transfer, ledger entry, key claim
or balance change, and the same key must then succeed as a first attempt. Running the workflow outside a
transaction makes all of these cases fail.

Each balance change is also atomic on its own. `UPDATE … SET balance = balance ± $amount` does the
read-modify-write inside Postgres, so it cannot lose a concurrent update.

The one step that can never be atomic with the commit is delivering the HTTP response. That gap is
exactly what idempotency closes: if the response is lost, the retry replays the committed result.

## 5. Idempotency

- **Storage:** `idempotency_records`, keyed by the client key. It survives restarts because it lives in Postgres.
- **Detection:** the primary-key conflict on insert. The database decides, not a check-then-insert in
  application code.
- **Payload binding:** we store `sha256(fromWalletId|toWalletId|amount)`. Reusing a key with a different
  payload is a client bug and is rejected, not silently replayed.
- **Returning the original result:** the record points to its transfer, and transfers are immutable once
  terminal. The replay reloads the transfer and returns the same status code and body. We store the domain
  result, not raw HTTP bytes, so the service layer stays independent of the transport.
- **Concurrent duplicates:** a second transaction inserting the same key **blocks on the unique index**
  until the first commits. Then `DO NOTHING` sees the committed row and the request replays. If the first
  rolls back, the second proceeds as the original. No duplicate side effects are possible either way.
- **Why the FK is deferrable:** the claim is inserted first, so the key is locked before any other work.
  At that point it references a transfer id that is not inserted until step 3. The FK is checked at commit,
  so `transfer_id` can still be `NOT NULL`.
- **What is not recorded:** validation errors, unknown wallets, lock timeouts and internal errors. Those
  roll back, and a retry is evaluated again. This is correct because no side effect happened.

## 6. Concurrency control

**Strategy:** pessimistic row locks (`SELECT … FOR UPDATE`) at `READ COMMITTED`.

- The funds check reads a balance **while holding its row lock**, so there is no read-then-write race.
  Two debits on the same wallet serialize, and the second sees the first's result.
- **Deadlocks are avoided by lock ordering:** both wallets are always locked in ascending id order, so
  A→B and B→A transfers queue instead of deadlocking. The idempotency claim is taken first. Only requests
  with the same key wait on it, and such a waiter holds nothing else, so no lock cycle can form.
- `CHECK (balance >= 0)` is a last line of defense if a future code path ever skips the lock.
- `lock_timeout` (default 3s) bounds how long a request waits on a hot wallet. Instead of piling up,
  the request fails with a retryable `503`.

**Alternatives considered:**

| Option | Why not chosen |
|---|---|
| Optimistic locking (version column) | Needs retry loops in the application, and performs worse on hot wallets with frequent conflicts |
| `SERIALIZABLE` isolation | Correct, but every caller must retry on `40001`. More moving parts for the same guarantee |
| Single conditional `UPDATE … WHERE balance >= $1` | Atomic for the debit, but makes it harder to record a FAILED transfer cleanly and to order locks across both wallets |

## 7. State machine

```
PENDING ──Process()──▶ PROCESSED   (terminal)
   └─────Fail(reason)─▶ FAILED      (terminal)
```

- The domain model rejects any other transition with `ErrInvalidTransition`.
- The repository update is guarded with `WHERE status = 'PENDING'` and must affect exactly one row. A
  duplicate or late transition therefore cannot overwrite a terminal state, even if a bug bypassed the
  domain check.
- With the single-transaction design, `PENDING` is never visible outside the transaction. It still exists
  so the lifecycle is explicit, and so the schema already supports a two-phase design (§11).

## 8. Failure modes and retry behavior

| Failure | Outcome | Client retry |
|---|---|---|
| Duplicate request (sequential) | Original result replayed | Safe |
| Duplicate request (concurrent) | Second waits, then replays | Safe |
| Commit succeeded but the response was lost | Retry replays the committed result | Safe, the intended path |
| Crash or connection loss before commit | Transaction rolls back, including the key claim | Safe, runs as new |
| Insufficient funds | `FAILED` recorded and replayed with the same `422` | Same result. Use a new key to try again after funding |
| Hot wallet lock wait exceeded | `503`, rolled back | Safe, with backoff |
| Same key with a different payload | `422 IDEMPOTENCY_KEY_REUSED` | Client bug, do not retry |
| Database unavailable | `500` / `503` on `/readyz` | Safe |

## 9. Observability

- Structured JSON logs (`log/slog`). An access log records method, path, status, duration and request id.
- `X-Request-ID` is accepted or generated, echoed in the response and attached to every log line.
- Every transfer outcome is logged with `transfer_id`, `idempotency_key`, `status` and `replayed`.
- Panics are recovered into `500` responses and logged with the stack.
- `/healthz` and `/readyz` back the Docker health checks.
- Metrics (Prometheus counters for outcomes, replays and lock timeouts) are the next step and are out of
  scope here.

## 10. Runtime topology and testing strategy

`docker compose up --build` starts three services:

- `db` runs Postgres 17, with a health check.
- `migrate` is a one-shot job that runs `wallet migrate --seed`. It starts after `db` is healthy.
- `app` runs `wallet serve`. It starts only after `migrate` completes successfully.

Migrations are embedded in the binary. They are tracked in `schema_migrations` and serialized by a
Postgres advisory lock, so concurrent runners are safe. The seed is idempotent.

Tests are behavioral, use real Postgres where correctness depends on it, and run in Docker with
`make test`:

- **domain:** state transitions, validation and ledger pair construction (pure unit tests)
- **handler:** request decoding, validation and error-to-status mapping (fake service)
- **service (integration):** success, insufficient funds, replay, key reuse, unknown wallet,
  and the ledger invariants
- **concurrency (integration):** many debits racing on one wallet; many requests with the same key;
  A→B and B→A in parallel without deadlock
- **atomicity (integration):** a crash injected after every write inside a transfer leaves no trace
- **migrator (integration):** re-running is a no-op; concurrent runs are safe
- **end-to-end:** a full HTTP round trip against the real stack

Integration tests need `TEST_DATABASE_URL` and skip with a clear message without it. Each test
package gets its own temporary database, so packages can run in parallel.

## 11. Tradeoffs and future work

- **Single transaction vs. two-phase.** Committing `PENDING` first and processing in a second transaction
  would expose in-flight transfers and require a recovery sweeper. That suits asynchronous or external
  settlement, but it adds failure modes without benefit for an internal ledger.
- **Global key scope.** Without authentication there is no client identity. With one, the key would become
  `(client_id, key)`.
- **Key retention.** Records are kept forever here. In production they would get a TTL (e.g. 24–72h)
  and a cleanup job.
- **Funding.** Seeded opening balances have no ledger entries. Production would fund wallets through
  transfers from a system or treasury account, so that balance = Σ ledger holds globally.
- **Hot wallets.** Row locks serialize all traffic on one wallet. At very high volume, consider sharded
  sub-balances or batching.
- **Events.** Downstream notification would use a transactional outbox table written in the same
  transaction.

### Orchestration (sagas, workflow engines): deliberately not used

A saga or workflow engine (for example Temporal) coordinates steps that **cannot share one ACID
transaction**. Here both wallets, the ledger and the idempotency record live in one Postgres database, so
a single transaction already gives the strongest guarantee available: all or nothing, isolated, with no
intermediate state visible. A saga would replace that with weaker eventual consistency. It would also add
new failure modes: compensations, visible `PENDING` transfers, a recovery worker and duplicate step
execution. Nothing would be gained in return.

It becomes the right tool when one of these is true:

| Trigger | Why one transaction no longer works |
|---|---|
| Wallets live in different databases, shards or services | No shared transaction spans them |
| Settlement involves an external system (bank, card network, blockchain) | The external call cannot be rolled back |
| The flow has slow or human steps (fraud review, approvals, holds) | A transaction cannot stay open for minutes or days |

The design would then evolve in place, without a rewrite:

1. **Commit the intent first:** write the transfer as `PENDING` plus a hold on the source funds and an
   outbox event, in one local transaction. The state machine and schema already allow `PENDING`.
2. **Run idempotent steps:** an orchestrator (a Temporal workflow, or a state-machine worker reading the
   outbox) executes each step. Every step is idempotent, keyed by the transfer id, and retried with
   backoff.
3. **Finish with a guarded transition:** each terminal state is set with the same guarded update used
   today (`WHERE status = 'PENDING'`), so retries and duplicates are harmless.
4. **Compensate, never delete:** a failure after money moved is undone by a reversing ledger pair. The
   ledger stays append-only.

## 12. Code structure (SOLID)

| Principle | Where it shows |
|---|---|
| **Single responsibility** | Handlers only translate HTTP. `TransferService` holds only the workflow. Idempotency lives in `idempotency.go`. Logging is a decorator (`LoggingTransfers`). Repositories only run SQL. Business rules live in the domain. |
| **Open/closed** | Cross-cutting concerns (logging today, metrics or tracing next) wrap `TransferUseCase` as decorators, without editing the workflow. Tests inject faults the same way. |
| **Liskov substitution** | Repositories behave identically on the pool or inside a transaction, so the service never knows which it has. Fault-injecting and logging wrappers substitute transparently. |
| **Interface segregation** | Ports are small and owned by their consumer. `WalletService` sees only `WalletReader`, so it can never lock or move money. The HTTP create and query handlers see `TransferCreator` and `TransferReader` respectively. |
| **Dependency inversion** | The service defines the interfaces it needs (`ports.go`). The Postgres adapter implements them. `internal/app` is the only place that wires concrete types together. |

The domain uses `Debit`/`Credit` with positive amounts, the ledger's own vocabulary. A signed
"adjust by −amount" call would let a sign error silently turn a debit into a credit.
