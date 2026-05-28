# Design Notes

## Overview

This service implements `POST /transfers` with:

- API-level idempotency (`idempotencyKey`)
- concurrency safety / no double spend
- double-entry ledger recording (DEBIT + CREDIT per transfer)
- stored wallet balances updated atomically

## Schema

See `db/migrations/001_init.up.sql` for full DDL.

Key constraints:

- `transfers.idempotency_key` is `UNIQUE` to prevent duplicate transfer execution by key.
- `ledger_entries` has `UNIQUE(transfer_id, entry_type)` to prevent duplicate debit/credit rows per transfer.
- `wallets.balance` has `CHECK (balance >= 0)`.

## Idempotency

`idempotency_records` stores:

- `idempotency_key` (PK)
- `request_hash` (hash of `{from,to,amount}`)
- `response_code` + `response_body` (durable replay payload)

Behavior:

- First request inserts the idempotency row in the same DB transaction as the transfer and stores the final response before commit.
- Duplicate request:
  - same `request_hash` → return stored response (exact replay)
  - different `request_hash` → `409 Conflict`

Concurrency note:

- The insert path uses `INSERT ... ON CONFLICT DO UPDATE ... RETURNING` so duplicate requests on the same key serialize on the idempotency row and the “loser” can safely replay the finalized response after the winner commits (no “in-flight” reads).

Optimization:

- Best-effort in-memory LRU+TTL cache for replaying finalized idempotency responses quickly.
- Cache is never used as a source of truth; DB is authoritative.

## Concurrency / No Double Spend

Transfers run in a single DB transaction and use a conditional atomic debit:

```sql
UPDATE wallets
SET balance = balance - $amount
WHERE wallet_id = $from AND balance >= $amount;
```

If the update affects 0 rows, the transfer fails with `INSUFFICIENT_FUNDS` and no ledger entries are written.

This prevents read-then-write races without requiring explicit `SELECT ... FOR UPDATE` in application code.

Deadlock avoidance:

- Before updating balances, the service locks the two wallet rows in a deterministic order (lexicographic wallet_id) to avoid circular-wait deadlocks when many transfers touch overlapping wallet pairs.

## Microservice Evolution

- Phase 1 (now): single service + single Postgres DB provides strong transactional guarantees.
- Phase 2 (extract services but keep shared DB): row-level locking/atomic updates still work because concurrency control lives in Postgres.
- Phase 3 (true microservices with DB-per-service): replace DB locking assumptions with reservation/hold + saga and outbox/inbox idempotent messaging (not implemented here).
