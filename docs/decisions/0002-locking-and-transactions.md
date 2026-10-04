# ADR 0002: One transaction, pessimistic wallet locks

## Status

Accepted

## Context

Two transfers can debit the same wallet at the same time. Retries can also arrive after a crash or a lost response. The implementation has to prevent double spends, lost updates, negative balances, and deadlocks.

## Decision

Run each `POST /transfers` attempt in a single `READ COMMITTED` transaction.

- Claim the idempotency key first, so two retries serialize on that unique index before they take wallet locks.
- Lock wallet rows with `SELECT ... FOR UPDATE` in ascending wallet id order.
- Debit with `UPDATE ... WHERE balance >= amount`, and keep `CHECK (balance >= 0)` as a backstop.
- Insert the transfer as `PENDING`, then transition it to `PROCESSED` or `FAILED` before commit.
- A deferred constraint trigger rejects a commit that is still `PENDING`, a processed transfer whose ledger is not a balanced pair, or a failed transfer that has ledger rows.

Isolation is `READ COMMITTED` because the row locks, not the isolation level, provide the critical section. `SERIALIZABLE` would also be correct and would force application retries on serialization failures. Row locks make the failure mode explicit: the second debit waits, then observes the reduced balance.

## Consequences

- Callers never read a committed `PENDING` transfer. `PENDING` still exists in the state machine and in the row that is updated inside the transaction.
- A crash before commit leaves no transfer and no idempotency row. The client retries the same key and the operation runs once.
- A crash after commit, if the response was lost, retries the same key and receives the stored transfer.
- Lock order prevents the `A -> B` versus `B -> A` deadlock.
- `lock_timeout` stops a waiter from blocking past the request budget. The client can retry.

## Alternatives

Optimistic version columns detect a lost update only after the write, and the use case must retry. That is a good fit for low contention. Wallet debits contend on hot rows, and a locked read of the balance is easier to explain and to test.

Deriving the balance only from the ledger avoids a stored balance drifting from the audit log. The assignment allows either approach. A stored balance makes the insufficient-funds predicate a single conditional update. The ledger trigger plus the reconciliation query keep the two representations honest.

Committing `PENDING` and processing it in a second transaction models a queue. It also creates a crash window and a recovery worker that this service does not need, because there is no external side effect between the two steps. Money movement and the ledger are the same database.
