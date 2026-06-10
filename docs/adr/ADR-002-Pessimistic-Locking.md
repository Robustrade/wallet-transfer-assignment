# ADR-002: Pessimistic Locking

## Context

Wallet balance updates must be correct under concurrent transfer requests. Multiple transfers touching the same wallet can race, causing lost updates or incorrect balances if concurrency is not controlled.

## Decision

Use pessimistic locking on wallet rows during transfer execution, acquiring `SELECT ... FOR UPDATE` locks within a transaction before adjusting balances.

## Alternatives Considered

- Optimistic locking with version numbers or compare-and-swap updates.
- Application-level mutexes or distributed locking outside the database.
- Serializable transaction isolation without explicit row locks.
- Queue-based transfer processing to serialize updates.

## Consequences

- Benefits:
  - Prevents concurrent debit/credit races on the same wallet.
  - Keeps concurrency handling inside the database transaction boundary.
  - Simplifies correctness for balance updates.
- Tradeoffs:
  - Can increase lock contention under high concurrency.
  - Transaction duration must be kept short to avoid blocking.
  - Requires careful ordering and retry handling for deadlocks.

## Implementation Status

This ADR is implemented in the service: wallet updates are protected by `SELECT ... FOR UPDATE` acquired inside a short transaction. The repository retains a `version` column on `wallets` for possible future optimistic-locking, but the current critical transfer path uses pessimistic locking for simplicity, correctness, and predictable behavior under contention.
