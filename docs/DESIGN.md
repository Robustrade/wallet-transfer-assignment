# Wallet Transfer Service — Design Notes

## Problem statement

Support wallet-to-wallet transfers with idempotent request handling, a double-entry ledger,
correct balances under concurrency, and a transfer state machine (`PENDING → PROCESSED` /
`PENDING → FAILED`).

## API contract

`POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "<uuid>", "toWalletId": "<uuid>", "amount": 100 }
```

| Outcome | Status | Notes |
|---|---|---|
| New transfer executed | `201 Created` | Body includes `status` (`PROCESSED` or `FAILED`). Insufficient funds is a business outcome, not a request error, so it is still a 201 with `status=FAILED`. |
| Replay of an existing `idempotencyKey` + identical payload | `200 OK` | Same body as the original response. |
| `idempotencyKey` reused with a different payload | `409 Conflict` | |
| Unknown wallet id | `404 Not Found` | |
| Validation failure (blank fields, non-positive amount, malformed UUID) | `400 Bad Request` | |

`GET /wallets/{id}` and `GET /transfers/{id}` are included as optional read endpoints for manual
verification and demos; they are not required by the assignment.

## Side effects

A successful transfer writes three rows in one transaction: the `transfers` row (status
`PROCESSED`) and two `ledger_entries` rows (one `DEBIT` on the source wallet, one `CREDIT` on the
destination), and updates both `wallets.balance` values. A `FAILED` transfer writes only the
`transfers` row — no ledger entries, no balance change — and that is committed, not rolled back.

## Idempotency strategy

`transfers.idempotency_key` is `UNIQUE`. There is no separate `idempotency_records` table — the
`transfers` row *is* the idempotency record. A `request_fingerprint` column (SHA-256 of
`fromWalletId|toWalletId|amount`, with amount normalized to the ledger's fixed scale) is stored
alongside it so a key reused with a different payload is detected as a conflict rather than
silently replayed.

Flow:
1. Look up the key first (fast path for the common replay case), with no transaction/lock held.
2. If absent, delegate to `TransferExecutor.execute(...)` (its own `@Transactional` method),
   which locks both wallets and only then `INSERT`s the transfer row as `PENDING` (see Concurrency
   strategy below for why insert comes after the lock). The unique constraint on
   `idempotency_key` is the actual correctness guarantee — if two requests with the same key race
   past the initial lookup simultaneously, the database accepts exactly one insert.
3. The loser's `INSERT` throws `DuplicateKeyException`, which propagates out of
   `TransferExecutor.execute` and rolls back that entire transaction (releasing its wallet locks
   cleanly — nothing was left half-done). `TransferService.transfer` catches it and re-reads the
   now-committed row, returning it as a replay (or a conflict, if the fingerprint disagrees).
4. This makes idempotency durable across process restarts: there's no in-memory cache to lose,
   the guarantee lives in a committed row.

## Concurrency strategy

Pessimistic row locks (`SELECT ... FOR UPDATE`), not optimistic locking. Each transfer locks its
source and destination wallet with two sequential single-row lock calls **in a globally
consistent order (ascending wallet id)** — not a single `IN (...)` query — so that locks are
acquired in that exact order regardless of which wallet is "from" and which is "to". This is what
prevents a deadlock when transfer A→B and transfer B→A run at the same time: both threads always
try to lock the lower-id wallet first.

**The transfer row is only inserted after both wallet locks are held**, not before. This was not
the first design tried — inserting the `PENDING` transfer row first (so the idempotency unique
constraint could be checked cheaply before doing any locking) seemed reasonable, but it caused a
real, reproducible deadlock under the concurrency test: `transfers.from_wallet_id`/`to_wallet_id`
are foreign keys, and Postgres takes an implicit `FOR KEY SHARE` lock on the referenced wallet row
during the `INSERT` to enforce that constraint. With many concurrent transfers between the same
two wallets, several transactions ended up each holding a `FOR KEY SHARE` lock (shared, so they
don't block each other) on the same wallet row from their own `INSERT`, then each tried to
upgrade to `FOR UPDATE` for the actual debit — and none could proceed because each was waiting on
the others' shared lock to be released, which only happens at commit. Locking before inserting
avoids this entirely: by the time the `INSERT`'s implicit FK lock is requested, the current
transaction already holds `FOR UPDATE` on that row itself, which is always permitted.

The balance check happens *after* the lock is held, not before — an earlier unlocked existence
check is allowed to be stale, but the balance the decision is based on never is. Default
`READ COMMITTED` isolation is sufficient since the row lock already serializes writers; no
serialization-failure retry loop is needed.

Optimistic locking (a `version` column) was considered and rejected: wallet transfers are
write-heavy and contention-prone on "hot" wallets, and `FOR UPDATE` gives predictable blocking
instead of retry storms, with one transaction boundary that's easy to reason about end to end.

## Failure modes

- **Insufficient funds**: committed `FAILED` transfer, reason recorded, no ledger entries, no
  balance change.
- **Unknown wallet**: `WalletNotFoundException` before any transfer row is written — an invalid
  request never leaves a `PENDING` row behind.
- **Idempotency key reused with a different payload**: `IdempotencyConflictException` → 409.
- **Concurrent duplicate submission of the same key**: the database's unique constraint
  guarantees exactly one transfer is ever created; every other concurrent caller observes it as a
  replay.

## Consistency expectations

- A transfer's two ledger entries always sum to zero (one `DEBIT`, one `CREDIT`, same amount) —
  enforced by `UNIQUE(transfer_id, entry_type)` plus the service always inserting both entries in
  the same transaction as the status update.
- `wallets.balance` can never go negative — enforced both by the pre-write check under the row
  lock and by a `CHECK (balance >= 0)` constraint as a backstop.
- `ledger_entries` can never reference a nonexistent transfer (`FOREIGN KEY`).

## Testing strategy

- Domain unit tests (no Spring context): valid/invalid `TransferStatus` transitions.
- Service integration tests (Testcontainers PostgreSQL, real constraints and locking, no
  mocking): happy path ledger balance, idempotent replay, idempotency conflict, insufficient
  funds, unknown wallet, concurrent overdraft attempts on one wallet, concurrent opposite-direction
  transfers (deadlock check), concurrent duplicate idempotency key submissions.
- Controller test (MockMvc + Testcontainers): the HTTP contract end to end, including the replay
  status code.

## Assumptions / tradeoffs

- No wallet-creation endpoint — out of scope per the assignment; wallets are pre-provisioned via
  a Flyway seed migration (`V2__seed_wallets.sql`) for manual testing.
- No separate `idempotency_records` table, as noted above — fewer moving parts, no risk of the
  idempotency record and the transfer disagreeing, at the cost of diverging from the assignment's
  suggested four-table schema.
- `GET /wallets/{id}` and `GET /transfers/{id}` were added as the assignment's listed optional
  enhancements, kept intentionally thin.
