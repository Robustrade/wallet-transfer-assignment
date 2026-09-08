# Design Document — Wallet Transfer Service

## 1. Problem Statement

Support synchronous wallet-to-wallet transfers with exactly-once semantics
at the API level, double-entry ledger recording, and correctness under
concurrent access — without over-building beyond what a reviewer needs to
see the engineering judgment behind the design.

## 2. API Contract

### `POST /transfers`

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

`amount` is an integer number of minor units (e.g. cents). Using integers
rather than floats avoids binary floating-point rounding errors, which is
non-negotiable for money.

Response (201 on first success, 200 on idempotent replay):

```json
{
  "transferId": "uuid",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "replayed": false
}
```

| Condition                                   | HTTP Status |
|----------------------------------------------|-------------|
| Success (first time)                        | 201         |
| Success (idempotent replay)                 | 200         |
| Missing key / same wallet / bad amount      | 400         |
| Wallet does not exist                       | 400         |
| Insufficient funds                          | 422         |
| Same idempotencyKey, different payload      | 409         |
| Unexpected/internal error                   | 500         |

`POST /wallets` and `GET /wallets/{id}` are included as thin convenience
endpoints so the API is exercisable end-to-end without hand-seeding the
database — they are not part of the core assignment requirements.

## 3. Side Effects & Consistency Boundary

A single successful `POST /transfers` call, inside one database
transaction, does all of the following atomically:

1. Claims the idempotency key.
2. Locks both wallets (in a fixed order — see §5).
3. Validates sufficient balance.
4. Inserts the `transfers` row.
5. Debits the source wallet, credits the destination wallet.
6. Inserts both `ledger_entries` rows.
7. Marks the transfer `PROCESSED`.
8. Stores the response for future idempotent replay.

Either all eight steps commit, or none do. There is no window where money
has left one wallet without landing in the other, and no window where a
transfer is marked `PROCESSED` without matching ledger entries.

## 4. Idempotency

**Storage**: `idempotency_records(idempotency_key PK, request_fingerprint,
status, transfer_id, response_status, response_body)`.

**Detection & replay protocol**:

- `request_fingerprint` is a SHA-256 hash of the semantically meaningful
  request fields (`from`, `to`, `amount`). This catches the case where a
  client reuses a key for a genuinely different request — a client bug,
  not a legitimate retry — and returns `409 Conflict` instead of silently
  doing the wrong thing.
- Claiming a key is `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING`.
  PostgreSQL's documented behavior for this statement is what makes it
  safe under true concurrency, not just sequential retries: if a second
  transaction tries to claim the same key while a first is still
  in-flight, the second **physically blocks inside the INSERT** until the
  first transaction commits or rolls back. Only then does it evaluate
  whether a conflict exists:
  - First transaction committed → conflict exists, second transaction
    reads back a fully-populated, `COMPLETED` row and replays its stored
    response verbatim.
  - First transaction rolled back → no conflict at all (the row was never
    persisted); the second transaction proceeds as the new claimant.
- This means concurrent duplicate delivery (two copies of the same request
  racing each other, e.g. from a retrying load balancer) resolves to
  exactly one execution, not "usually one, sometimes two if unlucky."
  Verified directly in
  `TestIntegration_ConcurrentDuplicateRequests_ExactlyOnce`.
- Failed transfers (e.g. insufficient funds, unknown wallet) are also
  completed into the idempotency record. Retrying a request that
  previously failed replays the *same* failure rather than either
  re-attempting from scratch or (worse) silently returning success.

**Why not just rely on `transfers.idempotency_key UNIQUE` alone?** That
constraint is kept too, as a schema-level backstop — but it can only tell
you a duplicate *insert* was rejected, not hand you back the original
response body, and it can't help for requests that fail before a
`transfers` row would even be created (see §6).

## 5. Concurrency Strategy

**Locking**: pessimistic row locks via `SELECT ... FOR UPDATE` on both
wallets, inside the transfer's transaction, isolation level `READ
COMMITTED`. This was chosen over optimistic locking (version column +
compare-and-swap) because transfers are a low-latency, high-contention
write path where retries under contention would themselves need to be
correctly implemented and would add client-visible latency variance; a
short-held row lock is simpler to reason about and test.

**Deadlock avoidance — lock ordering**: Two wallets are always locked in
ascending ID order, regardless of transfer direction. Without this, a
transfer A→B running concurrently with a transfer B→A can deadlock: A
locks wallet A then waits for wallet B, while B locks wallet B and waits
for wallet A. With a fixed global order, both transactions attempt to lock
the same wallet first, so one simply blocks behind the other — no cycle,
no deadlock.

**A subtler deadlock this design specifically avoids**: `transfers` has
foreign keys to `wallets`. Inserting a `transfers` row makes PostgreSQL
implicitly take a `FOR KEY SHARE` lock on both referenced wallet rows to
enforce referential integrity. If that insert happened *before* the
explicit `FOR UPDATE` locks, many concurrent transactions touching the
same hot wallet could each be holding a `FOR KEY SHARE` lock (shared locks
are mutually compatible) while all simultaneously trying to upgrade to
`FOR UPDATE` — a classic lock-upgrade deadlock. This was caught by the
concurrency integration test during development (see §8) and fixed by
sequencing the wallet locks strictly before the `transfers` insert, so the
FK check only ever re-affirms a lock the same transaction already holds
exclusively.

**Validated by** `TestIntegration_ConcurrentTransfers_NoDoubleSpend`: 20
goroutines simultaneously attempt 100-unit transfers out of a
1000-balance wallet; exactly 10 succeed, exactly 10 fail with
`insufficient funds`, and the final balance is exactly 0 — proving no
lost updates and no double-spend under real concurrent load, run with
`-race` enabled.

## 6. Database Design

See `migrations/001_init.sql` for the full schema and inline rationale.
Highlights:

- `wallets.balance BIGINT CHECK (balance >= 0)` — the database itself
  refuses a negative balance even if application logic has a bug.
- `transfers.idempotency_key UNIQUE` — schema-level backstop for
  exactly-once (see §4).
- `ledger_entries UNIQUE (transfer_id, wallet_id, type)` — a given
  transfer can never accumulate more than one debit or one credit leg,
  enforcing the double-entry invariant at the schema level, not just in
  application code.
- `idempotency_records.transfer_id` is nullable and decoupled from
  `transfers`, because a request can fail *before* a valid `transfers` row
  could exist (e.g. the wallet doesn't exist, so a row referencing it
  would violate the foreign key) — the outcome still needs to be stored
  for replay.

## 7. Architecture / Layering

```
internal/domain       — entities, state machine, validation. Zero deps.
internal/service      — orchestration, idempotency & concurrency policy.
                         Depends on domain + narrow repo interfaces (ports).
internal/repository/
  postgres            — SQL implementations of those interfaces.
internal/handler      — HTTP request/response mapping, status-code mapping
                         from domain errors. Depends on a narrow interface,
                         not the concrete service, so it's unit-testable
                         without a database (see transfer_handler_test.go).
cmd/server            — wiring only.
```

Repository interfaces are defined in the `service` package (the consumer),
not the `postgres` package (the implementer) — this is what lets
`service`'s own tests (and in principle a future in-memory implementation)
depend on nothing concrete.

## 8. Testing Strategy

- **Domain tests** (`internal/domain`): pure unit tests of validation
  rules, the state machine, and the ledger-balance invariant. No I/O.
- **Handler tests** (`internal/handler`): HTTP status-code mapping and
  JSON shape, against a fake service. No database.
- **Integration tests** (`internal/service`, build tag `integration`):
  run against a real PostgreSQL instance, exercising the actual
  repositories — real row locks, real `ON CONFLICT` semantics — because
  the properties under test (no double-spend, exactly-once under real
  concurrency) are exactly the properties an in-memory fake would tend to
  assume away rather than prove. Two real bugs were caught this way during
  development and are described in the commit history / §5-6:
  1. Replaying a previously *failed* idempotent request returned `nil`
     instead of the original error.
  2. The FK-triggered lock-upgrade deadlock described in §5.

Run:

```
make up               # starts local Postgres via docker-compose
make test              # fast unit tests, no DB
make test-integration  # full integration suite against Postgres
make test-race         # integration suite with -race
```

## 9. Observability

Structured JSON logs (`log/slog`) at the service layer for every processed
or failed transfer (`transfer_id`, `from`, `to`, `amount` or `reason`), and
at the HTTP layer for every request (`method`, `path`, `status`,
`duration_ms`). A production version would add per-route request-count and
latency metrics and a trace span per transfer, but structured logs cover
the baseline "what happened and why" need for this scope.

## 10. Deliberately Out of Scope

- Authentication/authorization — assumed handled by an upstream gateway.
- Async/queued processing — the transfer is small and fast enough to do
  synchronously in one transaction; introducing a worker queue would add
  complexity (and its own idempotency/ordering concerns) without a stated
  requirement for it.
- Multi-currency — amounts are assumed to be in a single, implicit
  currency's minor units.
