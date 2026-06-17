# Implementation Plan — Wallet Transfer Service

This is the build plan: the order of work, what each step delivers, and how it is
verified. The design rationale (schema, idempotency, concurrency, failure modes)
lives in [`DESIGN.md`](./DESIGN.md); this document is the *how and when*.

## Goal & constraints

- Deliver a reliable `POST /transfers` endpoint with idempotent handling, a
  double-entry ledger, correct balances under concurrency, and safe state
  transitions.
- Time-box: 3–5 hours. Optimise for **correctness and clarity**, not features.
- Stack fixed up front: Go + PostgreSQL, pgx/v5, raw SQL (no ORM), clean layered
  architecture (handler → service → repository → domain).

## Documentation-first

Following the assignment's preferred order, the spec/design note is written
**before** code. Each non-trivial behavior is pinned down first: expected
behavior, API contract, side effects, failure modes, idempotency behavior, retry
behavior, consistency expectations, and the testing strategy. That note is
`DESIGN.md`; the code is then made to match it.

## Phased plan

The phases are ordered so each builds on a tested foundation. Inner layers
(schema, domain) come first because everything depends on them; the service —
where the real difficulty lives — is built once its collaborators are stable.

### Phase 0 — Spec & design note
- Write `DESIGN.md`: schema, constraints, idempotency protocol, locking strategy,
  state machine, failure modes, testing strategy, assumptions/tradeoffs.
- **Done when:** every behavior the endpoint must exhibit is described in prose
  with the chosen mechanism and the alternative considered.

### Phase 1 — Schema & migrations
- `0001_init.sql`: `wallets`, `transfers`, `ledger_entries`, `idempotency_records`.
- Push invariants into the DB: `CHECK (balance >= 0)`, `CHECK (amount > 0)`,
  `CHECK (from_wallet_id <> to_wallet_id)`, foreign keys, and a deferred
  constraint trigger asserting each transfer's ledger entries net to zero.
- `0002_seed.sql`: demo wallets so transfers can run immediately.
- Embedded migration runner applies all `sql/*.sql` in order on startup.
- **Done when:** schema applies cleanly (and idempotently), and the constraints
  reject bad data when exercised directly in `psql`.

### Phase 2 — Domain
- Entities (`Wallet`, `Transfer`, `LedgerEntry`), `TransferStatus`/`EntryType`,
  money as `int64` minor units, and typed errors (`ErrWalletNotFound`,
  `ErrIdempotencyConflict`).
- No imports of pgx or net/http — pure vocabulary.
- **Done when:** the package compiles standalone and expresses the domain.

### Phase 3 — Repository
- One method per SQL statement, each taking an open transaction (`DBTX`), so the
  service owns the transaction boundary.
- Lock-aware operations (`LockWallets` with `FOR UPDATE`), guarded transitions
  (`MarkProcessed`/`MarkFailed` with `WHERE status='PENDING'`), and the
  idempotency operations (`ClaimIdempotencyKey` via `INSERT ... ON CONFLICT`).
- **Done when:** each method maps to exactly one well-understood query.

### Phase 4 — Service (the core)
- `CreateTransfer` orchestration in one transaction: hash the payload, claim the
  idempotency key, lock both wallets in sorted order, validate funds, insert
  `PENDING`, move money + write the two ledger entries, guarded transition to
  `PROCESSED` (or record `FAILED` on insufficient funds), finalize the
  idempotency record, commit.
- Duplicate handling: replay the stored response, or reject on payload mismatch.
- **Done when:** the behavioral tests in Phase 7 pass.

### Phase 5 — Handler
- Decode + structural validation (required fields, positive amount, no
  self-transfer), invoke the service, map results and typed errors to HTTP
  statuses. No business logic, no SQL.
- **Done when:** each documented status (201/400/404/422) is produced correctly.

### Phase 6 — Wiring
- `cmd/server/main.go`: config from env, pool, apply migrations, mount routes,
  serve. Health check. Graceful error handling.
- **Done when:** `go run ./cmd/server` boots, migrates, seeds, and serves.

### Phase 7 — Tests
- Behavioral tests against a real Postgres (testcontainers; or an existing DB via
  `WALLET_TEST_DATABASE_URL`).
- **Done when:** all the cases in the test plan below pass with `-race`.

### Phase 8 — Verify & document
- README (run/test instructions, API, PR description), reconcile derived vs
  stored balances, confirm observable behavior matches `DESIGN.md`.
- **Done when:** a reviewer can clone, run, and exercise the endpoint from the
  README alone.

## TDD discipline

Per the assignment's Red → Blue → Green:
- **Red:** write a failing test for the next missing behavior (e.g. "duplicate
  key returns the original response").
- **Blue:** implement the smallest change that makes it pass.
- **Green:** refactor while keeping tests green.

The idempotency and concurrency behaviors are written as tests first, because
they are the easiest to get subtly wrong and the hardest to eyeball.

## Test plan (what each test proves)

| Test | Proves |
|---|---|
| Happy path | money moves once; two balanced ledger entries; status `PROCESSED` |
| Idempotent replay | same key + body → one transfer, identical response |
| Idempotency conflict | same key, different body → 422, no money moved |
| Insufficient funds | recorded `FAILED`, no balance change, zero ledger entries; duplicate replays 422 |
| Wallet not found | 404, transaction rolled back |
| Ledger reconciliation | stored balance == balance derived from the ledger |
| Concurrency / no double-spend | N parallel debits on one wallet → exactly `floor(balance/amount)` succeed; balance never negative |

## Risks & mitigations

- **Concurrent double-spend** → pessimistic `FOR UPDATE` row locks; a parallel test asserts it.
- **Deadlocks** under crossing transfers → deterministic lock ordering by wallet id.
- **Duplicate side effects** on retry → INSERT-first idempotency keyed on a unique constraint, with payload hashing.
- **Transaction abort on duplicate insert** → `INSERT ... ON CONFLICT DO NOTHING` instead of catching the unique-violation error.
- **Ledger drift** → ledger as source of truth + reconciliation test + balanced-ledger trigger.

## Out of scope (documented, not built)

Auth, multi-currency, withdrawals/deposits, async/queue processing, and a
migration version table. Optional balance/history HTTP endpoints are easy
follow-ons over the repository read helpers if time allows.

## Definition of done

`POST /transfers` is idempotent, atomic, and concurrency-safe; the ledger always
balances; all tests pass with `-race`; and the README lets a reviewer run and
exercise it end to end.
