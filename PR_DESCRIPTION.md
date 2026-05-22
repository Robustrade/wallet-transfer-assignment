# Wallet Transfer Service — Solution

## Summary

Implements `POST /transfers` with exactly-once semantics, a double-entry
ledger, stored wallet balances, a `PENDING → PROCESSED | FAILED` state machine,
and race-free concurrent execution. Built in Go with a clean layered
architecture (handler → service → repository → domain) and PostgreSQL accessed
through `pgx`. The full transfer workflow is a single atomic transaction.

## AI disclosure

**Tool:** Claude Code (Claude Opus 4.7).

**How I used it:** as a design and review partner, not a code generator. I
drove every design decision through discussion and made the final calls myself;
the assistant explained trade-offs, scaffolded code to my specifications, and
helped write tests. A full transcript of the session is included in the repo.

The decisions I directed, in order, were:

1. **Domain grounding first.** Before any code, I asked how real bank transfer
   systems work and where they fail (double-entry, ACID vs. two-phase commit,
   sagas, reconciliation, idempotency). This shaped the scope and what to
   deliberately leave out.
2. **Technology choice.** I compared `pgx` vs. `sqlc` vs. `gorm` and chose
   `pgx` so transaction and lock behavior stays explicit and auditable rather
   than hidden behind an ORM.
3. **Concurrency strategy.** I worked through lock types and isolation levels
   and chose `SELECT ... FOR UPDATE` with deterministic lock ordering under
   `READ COMMITTED`, with a justification for not reaching for `SERIALIZABLE`.
4. **Balance model.** I weighed stored balance vs. derived-from-ledger and
   chose stored, because it allows a database-enforced `CHECK (balance >= 0)`
   as a last line of defence, with the ledger kept as the audit source.
5. **Schema portability.** I required standard SQL only (no PostgreSQL-specific
   types) so the schema is portable.
6. **Architecture correction.** An early refactor pushed the workflow into the
   repository; I caught that this violated the layering the assignment asks for
   and had it restructured to a unit-of-work pattern — workflow in the service,
   persistence-only in the repository — while keeping the behavior readable in
   one place.
7. **Test strategy.** I defined how each guarantee should be tested
   (invariant-based assertions, a barrier to maximize contention, money
   conservation, ledger reconciliation) and ran the integration tests against
   a real PostgreSQL via `DATABASE_URL`, with the tests skipping cleanly when
   no database is configured.

I reviewed all generated code and ran the full suite (including `-race` and
repeated runs) to confirm the behavior.

## Schema Design

Four tables: `wallets`, `transfers`, `ledger_entries`, `idempotency_records`.
Invariants are enforced in the database, not just in application code:

- `wallets`: `CHECK (balance >= 0)` prevents overdraft even under a logic bug.
- `transfers`: `CHECK status IN ('PENDING','PROCESSED','FAILED')`,
  `CHECK amount > 0`, `CHECK from_wallet_id <> to_wallet_id`.
- `ledger_entries`: `UNIQUE (transfer_id, type)` guarantees exactly one DEBIT
  and one CREDIT per transfer; a foreign key prevents orphan entries.
- `idempotency_records`: `idempotency_key` is the PRIMARY KEY, plus a
  `request_hash` to detect key reuse with a different payload.

Money is `BIGINT` minor units. The schema uses only standard SQL types.

## Idempotency Strategy

The client-supplied `idempotencyKey` is stored as a primary key in the same
transaction as the transfer. On a repeat request the existing record is found
and the original transfer is returned (the response is rebuilt from the
transfer row, so there is a single source of truth). A request hash detects key
reuse with different parameters and returns `409`. For concurrent duplicate
delivery, both requests may pass the initial lookup, but the PRIMARY KEY lets
only one insert succeed; the loser's transaction rolls back (no double
transfer) and it replays the winner's result.

## Concurrency Strategy

Each transfer locks both wallet rows with `SELECT ... FOR UPDATE`, acquired in
a deterministic order (by wallet ID) to avoid deadlocks between transfers that
touch the same pair. Concurrent debits of the same wallet therefore serialize:
each sees the committed balance of the previous one, so no lost update or
double spend is possible. `READ COMMITTED` is sufficient because correctness
comes from the row locks, avoiding the higher abort rate of `SERIALIZABLE`.
The `CHECK (balance >= 0)` constraint is the final backstop.

## How to Run

```bash
docker run --rm -d --name wallet-pg \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=wallet \
  -p 5432:5432 postgres:16-alpine
psql "postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable" -f migrations/0001_init.sql
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable"
go run ./cmd/server
```

## How to Test

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable"
go test ./... -race            # full suite (service tests need DATABASE_URL; skip if unset)
go test ./... -race -count=5   # stress the concurrency tests
```

Coverage: transfer execution, idempotent replay, key-reuse conflict,
insufficient-funds (`FAILED`), concurrent debits (no double spend, never
negative, ledger reconciles), and concurrent same-key delivery (exactly once).

## Tradeoffs / Assumptions

- **Stored balance over derived** for an atomic, DB-enforced overdraft guard;
  a test asserts stored balances reconcile with the ledger.
- **Pessimistic locking over optimistic** for simpler, retry-free handling of
  hot-wallet contention.
- **Single database**, so no two-phase commit / sagas — called out as a
  production concern beyond this assignment's scope.
- **Wallets are pre-seeded**; wallet creation, auth, and pagination are out of
  scope.
- **Failed transfers are persisted** rather than rolled back, so failures are
  auditable and idempotent.

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
