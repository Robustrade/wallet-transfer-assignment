> Hand-maintained, not generated. Every top-level module/package under `src/wallet_transfer/`
> needs a matching section here — see `AGENTS.md` "Architecture documentation". Diagrams are
> plain ASCII/Unicode box-drawing text in an untagged fenced code block, not Mermaid — update the
> diagram in the same PR as the code that changes it, not as a separate rendering step.

## Executive Summary

A wallet-to-wallet transfer service providing idempotent `POST /transfers`, a double-entry
ledger, and concurrency-safe balance updates over PostgreSQL. Two decisions shape everything
downstream. First: every transfer resolves to a terminal state — `PROCESSED` or `FAILED` — within
the single request and single database transaction that created it; there is no worker, queue, or
durably observable `PENDING` status (ADR-0008). Second: that same transaction always locks both
wallets, in ascending `wallet_id` order, *before* writing anything that references them — this is
what makes concurrent same-wallet debits and opposite-direction transfers safe without
deadlocking, and it's a stricter ordering requirement than it first looks (ADR-0002, ADR-0003):
inserting the transfer row before the explicit lock, which reads naturally, turns out to deadlock
under real Postgres because of an implicit FK lock — real-Postgres testing (ADR-0006) is what
caught it.

## System Overview

```
                       ┌───────────────────────────────────────────────┐
   HTTP POST           │           handlers  (FastAPI app)              │
   /transfers    ─────▶│  transfers.py: generated-model validation,     │
                        │  request → CreateTransferRequest,             │
                        │  service exception → HTTP status              │
                        └────────────────────┬────────────────────────┘
                                              │ CreateTransferRequest
                                              ▼
                        ┌───────────────────────────────────────────────┐
                        │           services  (TransferService)          │
                        │  one UnitOfWork per attempt:                   │
                        │  idempotency insert → lock wallets (ascending) │
                        │  → insert transfer → balance check →           │
                        │  ledger write → status update → commit,        │
                        │  bounded retry on lock contention              │
                        └────────────────────┬────────────────────────┘
                                              │ UnitOfWork / repository Protocols
                                              ▼
                        ┌───────────────────────────────────────────────┐
                        │  repositories  (Protocols + asyncpg impls)     │
                        │  WalletRepository · TransferRepository ·       │
                        │  LedgerRepository · IdempotencyRepository      │
                        └────────────────────┬────────────────────────┘
                                              │ SQL over an asyncpg pool
                                              ▼
                                     ┌──────────────────┐
                                     │    PostgreSQL      │
                                     │ wallets             │
                                     │ transfers           │
                                     │ ledger_entries      │
                                     │ idempotency_records │
                                     └──────────────────┘

   domain (Wallet, Transfer, LedgerEntry) has no arrow of its own — services and repositories
   both import it directly; it depends on nothing in this project.

   Deploy-time, not request-time (ADR-0004):
   Flyway ──migrate──▶ db/migrations/*.sql ──────────────▶ same PostgreSQL
          └─SEED_DEMO_DATA=true──▶ db/seed/*.sql (demo wallets only, ADR-0010)
```

## Components

### handlers

Purpose: HTTP transport only — request validation (via the generated Pydantic models),
converting a validated request into the service layer's own `CreateTransferRequest`, and mapping
each service-layer exception to the HTTP status ADR-0008 specifies. No business logic, no direct
repository access.

Public interface:
- `create_app(*, dsn: str | None = None) -> FastAPI` (`app.py`) — the app factory. `dsn` is only
  ever overridden by tests/the OpenAPI drift check; the running app reads `DATABASE_URL` lazily
  inside its lifespan, so building the app for schema introspection needs no reachable database.
- `POST /transfers` (`transfers.py`) — `InvalidTransferError` → 422, `WalletNotFoundError` → 404,
  `IdempotencyKeyReusedError` → 409, `RetryExhaustedError` → 503; a malformed body is rejected by
  the generated model before the route body even runs, and a custom `RequestValidationError`
  handler reshapes FastAPI's default error list into the same `{"detail": string}` envelope every
  other error response uses.
- `generated_models.py` — pure build output of `just generate-models` from `openapi/spec.yaml`
  (ADR-0005). Never hand-edited.

Depends on: `services`.

### services

Purpose: business orchestration — the idempotency check, the ordered wallet locks, the balance
check, the ledger write, and bounded retry under lock contention. The only layer that opens a
`UnitOfWork`/database transaction; the only layer that knows about retry policy.

Public interface:
- `TransferService.create_transfer(CreateTransferRequest) -> Transfer` — see the write order in
  the System Overview diagram above; the full reasoning is ADR-0002/ADR-0003.
- `services/errors.py` — `WalletNotFoundError`, `IdempotencyKeyReusedError`,
  `RetryExhaustedError`: the exceptions `handlers` maps to HTTP status codes.
- `services/fingerprint.py` — `compute_request_fingerprint()`, the SHA-256 of
  `(fromWalletId, toWalletId, amount)` that distinguishes a legitimate replay from idempotency-key
  misuse (ADR-0002).

Depends on: `repositories` (via its Protocols, not concrete `asyncpg` classes — dependency
inversion per ADR-0006, which is also what lets the service be TDD'd against the in-memory fake
in `tests/fakes/`), `domain`.

### repositories

Purpose: persistence only. Defines the four repository Protocols plus `UnitOfWork` — the
contracts `services` is written against — and their `asyncpg`-backed implementations. No workflow
decisions live here (e.g. no insufficient-funds branch); that's `services`' job.

Public interface:
- `WalletRepository`, `TransferRepository`, `LedgerRepository`, `IdempotencyRepository`,
  `UnitOfWork`/`UnitOfWorkFactory` — the async Protocols.
- `AsyncpgUnitOfWork` / `AsyncpgUnitOfWorkFactory` — the concrete implementation `handlers.app`
  wires up. Owns `SET LOCAL lock_timeout`, commit/rollback, and translating asyncpg's
  `LockNotAvailableError`/`DeadlockDetectedError` into this project's own retryable error types.
- `create_pool()` — the `asyncpg` connection pool factory, sized with headroom above CT6's
  100-concurrent-attempt ceiling (ADR-0003).
- `errors.py` — `RetryableRepositoryError` and its three concrete subclasses
  (`LockTimeoutError`, `DeadlockDetectedError`, `PoolTimeoutError`), plus
  `IdempotencyKeyConflictError`.

`AsyncpgWalletRepository.get_two_for_update()` is the one place worth a diagram — it issues two
*sequential* single-row `SELECT ... FOR UPDATE` statements in ascending id order, not one combined
query, because Postgres's `LockRows` plan node locks in scan order, not `ORDER BY` order:

```
get_two_for_update(a, b):
    for wallet_id in sorted({a, b}):        # ascending order is the actual mechanism
        SELECT ... WHERE id = wallet_id FOR UPDATE   # awaited before the next iteration starts
```

Depends on: `domain`.

### domain

Purpose: entities and their own construction/state-transition validation — pure, no I/O, no
dependency on anything else in this project.

Public interface:
- `Wallet` — `id`, `balance`, timestamps. No behavior beyond the shape itself; the `balance >= 0`
  invariant is enforced by the schema (ADR-0004), not re-checked here.
- `Transfer` — `Transfer.create(...)` rejects self-transfer and non-positive amount
  (`InvalidTransferError`) before any transaction opens; `mark_processed()`/`mark_failed(reason)`
  return a new instance and reject anything but `PENDING -> PROCESSED`/`PENDING -> FAILED`
  (`InvalidTransferTransitionError`). Immutable — a transition never mutates in place.
- `LedgerEntry` — `id`, `transfer_id`, `wallet_id`, `type` (`DEBIT`/`CREDIT`), `amount`,
  `created_at`.

Depends on: nothing else in this project.

## Known Limitations

<!-- Anything a reader would otherwise assume works but doesn't yet, or a shortcut taken
     deliberately. Keep this current — remove an item the moment it's resolved. -->

- **Observability is deliberately minimal.** No metrics/tracing infrastructure — out of scope for
  the time-box (`ASSIGNMENT.md` lists it under Optional Enhancements). Structured logging at the
  service-layer transaction boundary (attempt started, terminal outcome, retry count) is the only
  observability surface, sufficient to debug the scenarios this project actually covers.
- **Per-wallet throughput is serialized by design** (ADR-0003) — pessimistic row locking means a
  single wallet under extreme concurrent load (thousands of TPS) would need a different
  architecture (partitioned/sharded balance, or an async ledger-first design). Not solved here;
  named so it reads as a deliberate boundary, not an oversight.
- **No background worker or queue** (ADR-0008) — every transfer resolves within the request that
  created it; `PENDING` is never a durably observable status.
- **Single currency, implicit — multi-currency is explicitly out of scope.** `wallets`,
  `transfers`, and `ledger_entries` (ADR-0004) have no `currency` column: every wallet and every
  transfer is assumed to be the same, unspecified currency. No conversion, no per-currency
  balance, no check that a transfer's two wallets share a currency — there's nothing to compare.
  Not solved here; named explicitly so it reads as a deliberate boundary rather than something
  discovered by surprise later.
