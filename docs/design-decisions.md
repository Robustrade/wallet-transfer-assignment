# Design decisions and justification

Each decision lists what was chosen, the alternatives considered, and why.

## 1. Pessimistic row locks (`SELECT ... FOR UPDATE`) on both wallets
- **Alternatives:** optimistic locking with a version column; SERIALIZABLE isolation; atomic `UPDATE ... WHERE balance >= ?` only.
- **Why:** wallets can be hot (many debits on the same wallet). Optimistic locking would fail and retry under contention; SERIALIZABLE needs serialization-failure retry loops. A row lock makes the critical section tiny and deterministic. The balance check happens after the lock, so there is no read-then-write race.
- **Deadlock avoidance:** locks are always taken `ORDER BY id`, so A to B and B to A cannot deadlock.
- **Defence in depth:** guarded debit (`balance >= amount`) and `CHECK (balance >= 0)` in the database.

## 2. The `transfers` row is the idempotency record
- **Alternative:** a separate `idempotency_records` table (the assignment's suggestion).
- **Why:** key, request hash and result live in one row, committed in one transaction, so they cannot drift apart and there is one less write. The UNIQUE constraint on `idempotency_key` is the guarantee.
- **Trade-off:** if other operation types need idempotency later, a dedicated table is cleaner. Easy to add then.

## 3. Claim the key with `INSERT ... ON CONFLICT DO NOTHING`
- **Alternatives:** SELECT then INSERT (race); catch the unique-violation exception (aborts the Postgres transaction).
- **Why:** it is atomic, never aborts the transaction, and concurrent duplicates serialize on the unique index. The second request waits for the first to commit, then replays the stored result; if the first rolled back, the second proceeds normally.

## 4. Request hash and 422 on key reuse
- **Why:** the same key with a different amount or wallets is a client bug. Returning the old result would hide it, so it is rejected explicitly.

## 5. Insufficient funds is a stored `FAILED` result
- **Alternative:** return an error and store nothing.
- **Why:** the API promises the original result on retry. Replays of a failed transfer stay failed even if funds arrive later; a client that wants to try again uses a new key. Both are terminal states of the state machine.

## 6. Unknown wallet is a 404 that persists nothing
- **Why:** it is a client error, not a business outcome. The whole transaction, including the claimed key, rolls back, so the key can be reused after the wallet exists.

## 7. Stored balance plus ledger, written in one transaction
- **Alternative:** derive balance from the ledger (SUM on every read, or an additional snapshot).
- **Why:** O(1) balance reads and a simple lock target. The ledger still records every movement, and tests assert `balance = opening + credits - debits`.
- **Limitation:** opening balances are seeded, not ledger entries.

## 8. Double-entry enforced by constraints
- `UNIQUE (transfer_id, entry_type)` allows at most one DEBIT and one CREDIT per transfer, and both are written in the same transaction as the status change to PROCESSED, so a processed transfer has exactly two rows. Foreign keys prevent ledger rows without a transfer or wallet.

## 9. State machine in the domain plus a compare-and-set in SQL
- `Transfer.markProcessed/markFailed` reject illegal transitions in code; `UPDATE ... WHERE status = 'PENDING'` makes the transition safe under duplicate delivery.

## 10. HTTP semantics
- 201 first success, 200 replay, 422 for `FAILED` on first call and on replay (so a retry always sees the same code and body), 400 validation, 404 unknown, 409 conflicts.

## 11. Integer minor units and `idempotencyKey` required
- No floating-point money. Requiring the key keeps every mutating call retry-safe and the guarantee simple.

## 12. Real PostgreSQL in tests (Testcontainers), not H2
- Row locks, `ON CONFLICT` and CHECK constraints must behave exactly as in production; H2 would not prove the concurrency claims.
