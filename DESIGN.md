# Design — Wallet Transfer Service

> Original design outline, kept for history. See [`README.md`](./README.md#solution--wallet-transfer-service)
> for the as-built documentation — a few details below changed during implementation (noted inline).

## Stack
Go, chi router (net/http-based), PostgreSQL.

## Schema
- wallets(id, balance, created_at, updated_at)
- transfers(id, idempotency_key UNIQUE, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
- ledger_entries(id, transfer_id FK, wallet_id, entry_type, amount, created_at)

> **Change from the original outline:** no separate `idempotency_records` table. The unique
> constraint lives directly on `transfers.idempotency_key`. A second table would need to stay
> consistent with the transfer row it points to for no real benefit — `INSERT ... ON CONFLICT
> DO NOTHING` on `transfers` gives the same atomic dedup guarantee with less code.

## API
POST /transfers — see assignment spec for request/response shape.
GET /wallets/{id} — balance lookup (optional enhancement, used for manual testing).

## Idempotency strategy
Unique constraint on `idempotency_key`. A plain lookup short-circuits replays before any wallet
locking; a genuinely new key is claimed atomically via `INSERT ... ON CONFLICT DO NOTHING`, which
closes the race between two concurrent requests bearing the same brand-new key.

## Concurrency strategy
Row-level locking: `SELECT ... FOR UPDATE` on both wallets, always locked in a fixed order (sorted
by wallet id) to avoid deadlocks, inside a single DB transaction.

> **Change from the original outline:** the wallet locks must be acquired *before* the transfer
> row is inserted, not after. `from_wallet_id`/`to_wallet_id` are foreign keys, so inserting the
> transfer row takes an implicit shared lock on both wallets as part of the FK check. Locking
> after inserting let two concurrent transactions each hold that shared lock and then deadlock
> trying to upgrade to the exclusive `FOR UPDATE` lock the other held. This was caught by the
> concurrency test, not by inspection — see README's "How to Test" section.

## State machine
PENDING -> PROCESSED | FAILED. Transition happens once per transfer, guarded by the idempotency
check so retries never re-trigger a transition — including retries of a `FAILED` transfer, which
is treated as a valid terminal state.

## Testing plan
Unit tests for domain validation; integration tests (real Postgres) for transfer execution,
idempotent replay, insufficient funds, and two concurrency scenarios — same-direction parallel
transfers (lost-update check) and opposite-direction parallel transfers between the same wallet
pair (deadlock check).
