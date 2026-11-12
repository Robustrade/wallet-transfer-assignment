# Wallet Transfer Service — Design Note

This note is written **before** the implementation and kept up to date as the code evolves. It
records the contract, the guarantees and the reasoning behind the main decisions.

## 1. Problem statement

Move money between two wallets with:

- exactly-once effect at the API level when an `idempotencyKey` is supplied
- a double-entry ledger (one DEBIT + one CREDIT per processed transfer)
- correct balances under concurrent requests (no double spend)
- a safe transfer state machine (`PENDING -> PROCESSED | FAILED`)

## 2. Tech choices

| Concern     | Choice                                         | Why |
|-------------|------------------------------------------------|-----|
| Language    | Java 21, Spring Boot 3.5, Gradle               | Mainstream JVM stack; records and sealed types keep the domain small |
| Database    | PostgreSQL 16                                  | Preferred by the assignment; row-level locks and `ON CONFLICT` give us the primitives we need |

## 3. API contract

### `POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

| Outcome                                    | Status | Body |
|--------------------------------------------|--------|------|
| Transfer processed (first time)            | 201    | transfer |
| Replay of a processed transfer             | 200    | the **original** transfer, header `Idempotent-Replayed: true` |
| Insufficient funds (first time and replay) | 422    | transfer with `state: FAILED`, `failureReason: INSUFFICIENT_FUNDS` |
| Validation error (amount <= 0, same wallet, missing field, bad JSON, unknown field) | 400 | error |
| Wallet not found                           | 404    | error |
| Same key, **different** payload            | 409    | error `IDEMPOTENCY_KEY_REUSED` |

`idempotencyKey` is optional. Without it the request is executed every time (no deduplication is
possible; that is the documented semantic).

Error body: `{ "error": { "code": "...", "message": "..." } }`.


## 4. Data model

```text
wallets             (id PK, balance >= 0, opening_balance >= 0, created_at, updated_at)
transfers           (id PK, idempotency_key, from_wallet_id FK, to_wallet_id FK, amount > 0, state,
                     failure_reason, created_at, updated_at, CHECK from <> to, CHECK state IN (...))
ledger_entries      (id PK, transfer_id FK, wallet_id FK, type DEBIT|CREDIT, amount > 0, created_at,
                     UNIQUE (transfer_id, type))
idempotency_records (key PK, request_hash, transfer_id FK UNIQUE DEFERRABLE INITIALLY DEFERRED, created_at)
```

## 5. State machine

```text
PENDING --> PROCESSED
PENDING --> FAILED
```

`PROCESSED` and `FAILED` are terminal. Transitions are enforced twice: in the domain
(`Transfer.processed()/failed()` reject anything but `PENDING`) and in SQL
(`UPDATE ... WHERE id = ? AND state = 'PENDING'`, rows-affected checked), so a duplicate or retried
worker cannot flip a terminal transfer.

## 6. Transaction and concurrency strategy

The whole workflow runs in **one database transaction** at `READ COMMITTED`:

1. claim the idempotency key (if any) — see §7; replay and finish if it already exists
2. lock both wallets: `SELECT id FROM wallets WHERE id IN (:from, :to) ORDER BY id FOR UPDATE`
   (a missing row means unknown wallet → 404 and the whole transaction, including the claim, rolls back)
3. insert the `transfers` row (`PENDING`)
4. guarded debit: `UPDATE wallets SET balance = balance - :amt WHERE id = :from AND balance >= :amt`
    - 0 rows affected → insufficient funds → mark `FAILED`, commit (the outcome is recorded)
5. credit: `UPDATE wallets SET balance = balance + :amt WHERE id = :to`
6. insert the DEBIT and CREDIT ledger entries
7. mark `PROCESSED`, commit

Why this is safe:

- **Pessimistic row locks, taken in a fixed order.** `FOR UPDATE ... ORDER BY id` serialises
  transfers that touch the same wallet and makes `A->B` racing with `B->A` deadlock-free, because
  everyone locks the lower id first.
- **No read-then-write.** The funds check and the decrement are one atomic statement; even
  without the lock two concurrent debits could not both observe the old balance. The `CHECK
  (balance >= 0)` is the last line of defence.
- **Why pessimistic rather than optimistic locking?** Wallets are hot rows, and under contention
  optimistic versioning means retry storms. Locks make the common case a short wait and keep the
  logic simple. Transfers between disjoint wallets still run fully in parallel.
- A crash before commit rolls everything back; there is no partially applied transfer. A crash
  after commit is covered by idempotency (the client retries with the same key and receives the
  stored result).

## 7. Idempotency

- **Storage:** `idempotency_records(key PK, request_hash, transfer_id)`, written in the *same*
  transaction as the transfer, so a record exists iff the transfer does. The FK to `transfers` is
  `DEFERRABLE INITIALLY DEFERRED` so the key can be claimed first and the transfer inserted later
  in the same transaction.
- **Detection and race handling:** `INSERT ... ON CONFLICT (key) DO NOTHING`.
    - 1 row inserted → this request owns the key and executes the transfer.
    - 0 rows → another request owns it: load its record and return the original result.
    - If two requests race with a new key, PostgreSQL makes the second `INSERT` **wait** on the
      unique index until the first commits or rolls back. After a commit the second sees a conflict
      and replays; after a rollback it simply becomes the owner. No application-level retry loop or
      exception juggling is needed.
- **Same result:** replay returns the stored transfer (including a stored `FAILED`).
- **Payload mismatch:** `request_hash` = SHA-256 over (from, to, amount); a different payload under
  the same key → 409 rather than silently returning an unrelated result.
- **No duplicate side effects:** a replay never reaches the lock/debit/credit steps.
- **Restarts:** all state is in the database, so this survives process restarts.
- **Failed transfers are final for that key.** To retry after insufficient funds the client uses a
  new key (the usual convention for idempotent APIs).

- **Same key, different payload (`request_hash` differs):** reject with `409
  IDEMPOTENCY_KEY_REUSED`. Nothing is mutated — this request never reaches step 2.
