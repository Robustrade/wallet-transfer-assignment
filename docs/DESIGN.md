# Design: Wallet Transfer Service

This document is the design-first writeup for the service. It explains the
problem, the API contract, the data model and why each constraint exists, and
then the two mechanisms that carry most of the weight: idempotency
(exactly-once) and concurrency (no double-spend, no deadlock).

---

## 1. Problem statement

Provide an HTTP service that transfers money between wallets such that:

1. A transfer is **atomic** — either both wallets move and a balanced ledger is
   written, or nothing changes.
2. Concurrent transfers **never** oversend, lose updates, or corrupt a balance.
3. Clients can **safely retry** a create request; a duplicate must not move money
   twice, and that guarantee must survive process restarts.
4. Every successful transfer is **auditable** via a double-entry ledger.

Money is `int64` in the smallest currency unit (e.g. cents). No floating point
is used anywhere in the money path.

---

## 2. API contract

| Method & path                 | Success                              | Errors                                                        |
| ----------------------------- | ------------------------------------ | ------------------------------------------------------------- |
| `POST /transfers`             | `201` fresh / `200` replay           | `400` invalid · `409` key reuse · `422` insufficient funds    |
| `GET /transfers/{id}`         | `200`                                | `404`                                                         |
| `GET /wallets/{id}/balance`   | `200` `{walletId,balance}`           | `404`                                                         |
| `GET /healthz`                | `200` `{status:"ok"}`                | —                                                             |

Request body for `POST /transfers`:

```json
{ "idempotencyKey": "abc", "fromWalletId": "w1", "toWalletId": "w2", "amount": 100 }
```

Behavioral notes:

- A successful **replay** sets the response header `Idempotent-Replayed: true`.
- `422` returns the durable `FAILED` transfer **alongside** the error envelope,
  so the client sees both the outcome and the reason.
- All errors share one envelope: `{"error":{"code":"...","message":"..."}}`.
  Codes are stable and machine-readable (`invalid_request`, `not_found`,
  `insufficient_funds`, `idempotency_key_reuse`, `internal_error`). Internal
  errors are logged server-side and never leak detail to the client.

Transport hardening: the JSON decoder is strict (`DisallowUnknownFields`),
rejects trailing content (single object only), and bounds the body to 1 MiB.

---

## 3. Data model and rationale

PostgreSQL. Money columns are `BIGINT` minor units; invariants are enforced at
the database boundary so a logic bug cannot persist impossible state.

### `wallets`
`id TEXT PK`, `balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0)`,
`created_at`, `updated_at`.

- `CHECK (balance >= 0)` is the **last-line overdraft guard**. The service also
  checks funds before debiting, but the constraint guarantees no code path can
  ever commit a negative balance.

### `transfers`
`id UUID PK`, `idempotency_key TEXT NOT NULL UNIQUE`,
`from_wallet_id`/`to_wallet_id TEXT NOT NULL REFERENCES wallets`,
`amount BIGINT NOT NULL CHECK (amount > 0)`,
`status TEXT CHECK (status IN ('PENDING','PROCESSED','FAILED'))`,
`failure_reason TEXT NOT NULL DEFAULT ''`, timestamps,
`CHECK (from_wallet_id <> to_wallet_id)`.

- `UNIQUE (idempotency_key)` is a **defensive** second enforcement point for
  exactly-once (the primary one is the `idempotency_records` PK). Two layers of
  the same invariant is cheap insurance.
- `CHECK (amount > 0)` rejects zero/negative transfers at the boundary.
- `CHECK (from <> to)` makes self-transfers structurally impossible.
- FKs guarantee a transfer can never reference a non-existent wallet.

Indexes `transfers(from_wallet_id)` and `transfers(to_wallet_id)` support
per-wallet history/lookup queries without table scans.

### `ledger_entries`
`id UUID PK`, `wallet_id FK`, `transfer_id FK`,
`type TEXT CHECK (type IN ('DEBIT','CREDIT'))`,
`amount BIGINT CHECK (amount > 0)`, `UNIQUE (transfer_id, type)`.

- `UNIQUE (transfer_id, type)` allows **at most one DEBIT and one CREDIT per
  transfer**. This is what makes the double-entry write idempotent: even a buggy
  retry inside a transaction cannot produce duplicate or unbalanced ledger rows.
- Indexes `ledger_entries(wallet_id)` and `ledger_entries(transfer_id)` support
  reconciliation ("all entries for a wallet") and per-transfer audit.

### `idempotency_records`
`key TEXT PK`, `request_hash TEXT`, `transfer_id FK`, `response_body JSONB`,
`status TEXT CHECK (status IN ('PENDING','COMPLETED'))`, timestamps.

- `key` is the **PRIMARY KEY** — the single serialization point for duplicate
  detection (see §4).
- `request_hash` (sha256 of `from|to|amount`) detects the "same key, different
  payload" client error.
- `response_body JSONB` stores an auditable snapshot of the transfer outcome so
  a replay returns the original result without re-reading mutable state.

### Migrations
Embedded in the binary (`go:embed`) and applied on startup, each in its own
transaction, tracked in a `schema_migrations` table. Idempotent across restarts;
a fresh database is provisioned automatically with no manual step.

---

## 4. Idempotency design (exactly-once)

The client sends a unique `idempotencyKey` per logical transfer and may retry
freely. The mechanism:

1. Inside the transfer transaction, the service claims the key with
   `INSERT INTO idempotency_records ... ON CONFLICT (key) DO NOTHING`, with
   `status = PENDING`.
2. **First request** wins the insert (one row affected), performs the transfer,
   writes the response snapshot, and sets `status = COMPLETED`. Commit makes both
   durable together.
3. **Duplicate detection** is driven by **zero rows affected** on that insert,
   translated to a domain sentinel and handled as "replay".

> **Why `ON CONFLICT` and not a caught unique violation?** A raised unique
> violation (SQLSTATE 23505) *aborts the surrounding transaction* (`25P02`),
> which would make the subsequent replay read impossible without a savepoint.
> `ON CONFLICT DO NOTHING` reports the conflict via the row count while keeping
> the transaction healthy — and it preserves the same blocking semantics below.

### In-flight duplicates (the blocking mechanism)

When two requests with the same key race:

- Request A inserts the `PENDING` record but has **not committed** yet.
- Request B attempts the same insert. Even with `ON CONFLICT`, Postgres makes B
  **block on the unique index** until A's transaction resolves.
- When A commits, B's insert affects **zero rows**. B then reads the now
  `COMPLETED` record and **replays A's stored result**. Money moved exactly once.
- (If A had rolled back, B's insert would succeed and B would execute the
  transfer itself — still exactly-once.)

This is why correctness does not depend on any application-level lock or
single-process assumption: the database's unique constraint is the
serialization point.

### Restart safety and replay

Because the record is durable and written in the same transaction as the
transfer, a retry after a crash sees a `COMPLETED` record and replays. The
replay returns the stored snapshot and sets `Idempotent-Replayed: true`.

### Key reuse with a different payload

If the incoming `request_hash` differs from the stored one, the key is being
misused for a different operation. That is a client error → `409 Conflict`
(`idempotency_key_reuse`), and nothing is mutated.

---

## 5. Concurrency design (no double-spend, no deadlock)

### Transaction boundary

One transfer = one `WithinTx` call = one Postgres transaction. The transaction
is threaded through `context.Context`, so every repository call inside the
closure automatically participates — balances, ledger, transfer status, and the
idempotency record all commit or roll back as a unit.

### Row locking with `SELECT ... FOR UPDATE`

Both wallets are read with `FOR UPDATE`, taking exclusive row locks for the life
of the transaction. Funds are checked **after** the lock is held, so the
classic read-modify-write race ("two transfers both read balance 100, both
debit 80") cannot occur — the second transfer waits, then re-reads the updated
balance.

### Lock ordering → deadlock avoidance

Locks are acquired in **sorted wallet-id order**, not request order. Consider
`A→B` and `B→A` running concurrently:

- Without ordering: T1 locks A then waits for B; T2 locks B then waits for A —
  classic deadlock.
- With sorted ordering: both transactions lock `min(A,B)` first, so one simply
  waits for the other. No cycle, no deadlock.

The `balance >= 0` `CHECK` remains as a final backstop even though the funds
check already prevents overdraft.

### Lock before insert (foreign-key lock interaction)

Sorted lock ordering is necessary but **not sufficient**. Inserting a `transfers`
row takes a `FOR KEY SHARE` lock on both referenced wallet rows (PostgreSQL holds
this to stop a concurrent delete of the parent). If the transfer were inserted
*before* the `FOR UPDATE` acquisition, many concurrent transfers would each hold
a shared lock on the same wallet and then all try to upgrade to exclusive —
a lock-upgrade deadlock (`SQLSTATE 40P01`), even with consistent ordering.

The fix is ordering of operations within the transaction: **acquire the
`FOR UPDATE` locks first, then insert the transfer.** The stronger exclusive lock
is already held when the foreign-key share lock is taken, so there is nothing to
upgrade and nothing to deadlock on. This was caught by the concurrent-debit
end-to-end test against real Postgres (the in-memory fakes could not surface it),
which is precisely why those tests run against a real database.

### Insufficient funds is a durable, idempotent failure

When the source cannot cover the debit, the service does **not** abort the
transaction. It marks the transfer `FAILED` with a reason, finalizes the
idempotency record, and **commits**. The API returns `422` with the failed
transfer. A retry of the same key replays that `FAILED` outcome — a failure is
exactly-once too, never silently retried into a different result.

### Why pessimistic over optimistic (tradeoff)

Optimistic concurrency (version column + compare-and-swap, retry on conflict)
shines under low contention and read-heavy load. This is a **write-heavy,
correctness-critical money path** where two transfers touching the same wallet
genuinely conflict and must serialize. Pessimistic `FOR UPDATE`:

- expresses the intent directly ("I will modify these rows"),
- removes retry loops and the subtle bugs they invite,
- composes cleanly with the single-transaction double-entry write.

The cost is reduced parallelism for transfers sharing a hot wallet — acceptable,
and arguably correct, since those operations must serialize anyway. Deterministic
lock ordering removes the only real hazard (deadlock). If a future workload had
extreme contention on one wallet, sharding balances or a queue per wallet would
be the next step — out of scope here.

---

## 6. Transfer state machine

```
            create
              │
              ▼
          ┌────────┐   funds ok     ┌───────────┐
          │ PENDING│ ─────────────▶ │ PROCESSED │  (terminal, success)
          └────────┘                └───────────┘
              │
              │ insufficient funds
              ▼
          ┌────────┐
          │ FAILED │  (terminal, durable failure)
          └────────┘
```

`PENDING → PROCESSED` and `PENDING → FAILED` are the only legal transitions;
both target states are terminal. Illegal transitions return
`ErrInvalidStateTransition`. Terminal states are why a replay is always safe:
the outcome can never change after commit.

---

## 7. Failure modes & retry-safety

| Scenario                              | Behavior                                                                 |
| ------------------------------------- | ------------------------------------------------------------------------ |
| Client retries a successful transfer  | Replay stored `PROCESSED` snapshot, `200` + `Idempotent-Replayed: true`. |
| Client retries a failed transfer      | Replay stored `FAILED` snapshot, `422`. No re-execution.                 |
| Two identical requests race           | One executes, the other blocks on the unique index then replays.         |
| Same key, different payload           | `409`, nothing mutated.                                                  |
| Crash mid-transaction (before commit) | Transaction rolls back; nothing persisted; a retry executes cleanly.     |
| Crash after commit, client retries    | Durable record replays the committed outcome.                            |
| Insufficient funds                    | Durable `FAILED` transfer committed; `422`.                              |
| Unknown wallet / transfer             | `404`; no mutation.                                                       |
| Malformed / oversized body            | `400`; decoder is strict and bounded.                                    |

---

## 8. Observability

- Structured JSON logs via `slog` from the entrypoint; chi middleware adds
  request id, real IP, request logging, and panic recovery.
- Internal (`500`) errors are logged with method/path and the underlying error;
  the client only ever sees a generic message.
- `GET /healthz` is a liveness probe; the Compose stack additionally gates the
  app on a Postgres `pg_isready` healthcheck.
- Migrations log on apply. Startup work (connect + migrate) is bounded by a
  30s context so a wedged database fails fast instead of hanging.
- Natural next steps (out of scope): Prometheus metrics (transfer
  counts/latency, replay rate), distinct readiness vs liveness endpoints, and
  trace propagation.

---

## 9. Testing strategy

- **Unit — domain**: money arithmetic, amount validation, the transfer state
  machine, ledger construction. Pure, fast, no I/O.
- **Unit — service**: the `TransferService` orchestration against in-memory
  fakes of every port, covering validation, fresh success, replay, key reuse,
  and the insufficient-funds failure path.
- **Unit — handler**: HTTP status mapping, strict JSON decoding, the error
  envelope, and the `Idempotent-Replayed` header.
- **Integration**: the pgx/v5 adapters run against a real `postgres:16-alpine`
  started by testcontainers, with the actual migrations applied — exercising the
  real `FOR UPDATE` locking and unique-constraint behavior rather than fakes.
- **Concurrency**: end-to-end tests fire concurrent and duplicate transfers to
  assert exactly-once and no-double-spend hold against real Postgres, and that
  opposing transfers do not deadlock.
- **Skip-safe**: when Docker is unavailable the integration suite self-skips
  (testcontainers cannot reach a Docker host), so `go test ./...` is green
  everywhere and fully exercised in CI / on a Docker host. Tests run with
  `-race`.

---

## 10. Assumptions & tradeoffs

- **Single currency.** No currency field; all wallets share one unit. Multi-
  currency would add a currency column and forbid cross-currency transfers.
- **Integer minor units.** Exact arithmetic; no floats. Callers convert to/from
  display units.
- **No authentication / authorization.** Out of scope; a real deployment would
  put authn/z and rate limiting in front.
- **Wallets are pre-provisioned.** There is intentionally no wallet-creation
  endpoint; wallets are seeded out of band (SQL/admin). This keeps the surface
  focused on the transfer problem the assignment targets.
- **No partial transfers / holds / reversals.** A transfer is a single
  all-or-nothing movement; reversals would be modeled as compensating transfers.
- **Pessimistic locking** trades some parallelism on hot wallets for simplicity
  and obvious correctness (see §5).
- **At-least-once delivery assumed from clients**, hence the idempotency
  contract; clients are expected to supply a stable key per logical transfer.
- **Liveness only** for health; readiness/metrics are noted as next steps.
