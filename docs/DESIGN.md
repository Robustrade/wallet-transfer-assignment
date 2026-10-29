# Wallet Transfer Service — Design Note

Written before the implementation (documentation-first workflow).

## Problem and expected behaviour

Move money between two wallets with exactly-once semantics at the API level. Money is an integer in
minor units (`BIGINT`); no floating point.

## API contract

| Endpoint | Purpose | Responses |
|---|---|---|
| `POST /transfers` | Create a transfer. Body: `idempotencyKey`, `fromWalletId`, `toWalletId`, `amount` (>0) | `201` processed; `200` replay of a processed transfer; `422` `FAILED` (insufficient funds) on first call **and** every replay; `422 IDEMPOTENCY_KEY_REUSED`; `400` invalid; `404` unknown wallet |
| `GET /transfers/{id}` | Transfer + status | `200`, `404` |
| `GET /transfers/{id}/ledger-entries` | The two ledger rows | `200`, `404` |
| `POST /wallets` | Create wallet (`id`, `openingBalance`) — test/setup helper | `201`, `409` |
| `GET /wallets/{id}` | Balance | `200`, `404` |
| `GET /wallets/{id}/transfers` | History (optional) | `200`, `404` |

`idempotencyKey` is **required** (simplifies the guarantee; every mutating call is retry-safe).

## Schema (see `V1__init_schema.sql`)

* `wallets(id PK, balance CHECK >= 0)` — stored balance, updated in the same transaction as the ledger.
* `transfers(id PK, idempotency_key UNIQUE, request_hash, from/to FK, amount CHECK > 0, status CHECK IN (...), failure_reason)`
  with `from <> to`, and `failure_reason` present iff `FAILED`.
* `ledger_entries(id, wallet_id FK, transfer_id FK, entry_type, amount CHECK > 0, UNIQUE(transfer_id, entry_type))`
  — at most one DEBIT and one CREDIT per transfer, so a processed transfer has exactly two rows. FKs
  mean ledger rows cannot exist without a transfer or wallet.
* Indexes: unique index on `idempotency_key` (dedupe lookup), `(from_wallet_id, created_at)` and
  `(to_wallet_id, created_at)` for history, `(wallet_id, id)` on the ledger.

**Deviation from the suggested tables:** there is no separate `idempotency_records` table. The
`transfers` row *is* the idempotency record (key, request hash and result live together), which
removes a second write and any chance of the two drifting apart. Trade-off: a different transfer type
later would need its own key space; a dedicated table can be added then.

## Transfer flow (one DB transaction, READ COMMITTED + row locks)

1. Validate (amount > 0, wallets differ) — domain rule, nothing written yet.
2. `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING` a `PENDING` transfer.
   * 0 rows inserted ⇒ key seen before ⇒ load stored transfer, compare `request_hash`
     (mismatch ⇒ `IDEMPOTENCY_KEY_REUSED`), return it as a replay. No money moves.
3. `SELECT ... FROM wallets WHERE id IN (from, to) ORDER BY id FOR UPDATE`.
   Missing wallet ⇒ exception ⇒ whole transaction (including the claimed key) rolls back.
4. Balance < amount ⇒ `PENDING -> FAILED` (`INSUFFICIENT_FUNDS`), committed so retries get the same answer.
5. Else guarded `UPDATE` debit and credit, insert DEBIT + CREDIT ledger rows, `PENDING -> PROCESSED`.

## Idempotency

* **Stored**: `transfers.idempotency_key` with a UNIQUE constraint; durable and process-restart safe.
* **Detected**: the insert itself. The unique index makes two concurrent inserts of one key serialize:
  the second blocks until the first commits (then sees the row and replays) or rolls back (then
  inserts and proceeds).
* **Original result returned**: the stored transfer (status, ids, failure reason) is read back.
* **Response lost after commit**: the client retries with the same key and gets the stored result.
* **Same key, different body**: rejected with 422 instead of silently returning an unrelated result.
* **Failed transfers are results too**: replays of a `FAILED` transfer stay `FAILED` even if funds
  arrive later; the client must use a new key to retry intentionally.

## Concurrency

* Pessimistic row locks (`FOR UPDATE`) on both wallets. Chosen over optimistic locking because
  contention on hot wallets is expected, the critical section is tiny, and it never needs
  retry loops. Over SERIALIZABLE because it avoids serialization-failure retries.
* Locks are taken in `ORDER BY id`, so A→B and B→A cannot deadlock.
* Balance check happens *after* the lock, so there is no read-then-write race.
* Defence in depth: `balance >= amount` in the debit `UPDATE`, `CHECK (balance >= 0)`, and the
  compare-and-set `UPDATE transfers ... WHERE status = 'PENDING'` for state transitions.

## State machine

`PENDING -> PROCESSED` and `PENDING -> FAILED`; both terminal. Enforced in the domain
(`Transfer.markProcessed/markFailed`) and again in SQL by the CAS update.

## Failure modes

| Failure | Outcome |
|---|---|
| Crash/exception before commit | Everything rolls back, incl. key claim; retry starts clean |
| Crash after commit, response lost | Retry replays stored result |
| Duplicate concurrent requests | One executes, rest replay |
| Insufficient funds | Persisted `FAILED`, no ledger, no balance change |
| Unknown wallet | 404, nothing persisted |
| DB deadlock/lock timeout | Transaction aborted, client retries with same key (safe) |

## Consistency expectations / invariants (tested)

* For every wallet: `balance = opening balance + credits - debits`.
* Total money in the system is conserved; sum(DEBIT) = sum(CREDIT) per transfer.
* One key ⇒ at most one transfer ⇒ at most two ledger rows.

## Observability

Structured log lines on transfer outcomes (`PROCESSED`, `FAILED`, replay) with transfer id. Not
included (future): Micrometer counters, trace ids.

## Testing strategy

Real PostgreSQL via Testcontainers (not H2) so locks, `ON CONFLICT` and CHECK constraints behave as in
production. Domain unit tests for the state machine; service-level integration tests for ledger,
idempotency, failures and concurrency (12 identical concurrent requests, 20 racing debits on a wallet
with funds for 10, opposite-direction transfers); HTTP tests for status codes and replay.

## Known limitations

* No auth, currencies, or transfer limits.
* Opening balances are not ledger entries (wallets are seeded); the invariant is stated relative to the opening balance.
* Idempotency records are never expired.
