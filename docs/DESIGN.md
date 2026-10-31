# Wallet Transfer Service – Design Note (Node.js + PostgreSQL)

## 1. Problem statement

An HTTP service that moves money between two wallets with:

* idempotent request handling (exactly-once effect at the API level when an `idempotencyKey` is supplied),
* a double-entry ledger (every processed transfer = one DEBIT + one CREDIT of the same amount),
* correct balances under concurrent requests (no double spending),
* a safe transfer state machine (`PENDING → PROCESSED | FAILED`).

Non-goals: authentication, currencies/FX, fees, pagination, multi-region deployment.

## 2. Key decisions (and why)

| Topic | Decision | Reason |
|---|---|---|
| Stack | Node.js, built-in `node:http`, `pg` driver, built-in `node:test` | One runtime dependency; small surface. |
| Database | **PostgreSQL 16** (Docker Compose provided) | Row-level locking, real concurrency, rich constraints/triggers. |
| Money type | `bigint` minor units (e.g. paise/cents), `> 0`, max 10^15 | No floating point; fits a JS safe integer (parsed via a type parser). |
| Balance | **Stored** `wallets.balance`, updated in the same transaction as the ledger rows | O(1) reads; the ledger is the audit trail, cross-checked by `checkIntegrity()`. |
| Opening balance | `wallets.initial_balance` column (not a ledger entry) | Keeps "Σ debits == Σ credits" true without a synthetic system wallet. Invariant: `balance = initial_balance + Σcredits − Σdebits`. |
| Concurrency | `READ COMMITTED` transaction; lock **both wallet rows** with `SELECT … FOR UPDATE ORDER BY id`; guarded `UPDATE … WHERE balance >= :amount` | Only the two wallets involved are locked (high throughput); sorted lock order makes A→B / B→A deadlock-free; the guarded update plus `CHECK (balance >= 0)` are extra safety nets. |
| Idempotency | `idempotency_records(key PRIMARY KEY, request_hash, transfer_id)` in the **same transaction** as the transfer, plus `pg_advisory_xact_lock(hash(key))` | Atomic with the transfer; the advisory lock serialises concurrent same-key requests so "not found → execute" cannot happen twice. PK is the backstop. |
| Insufficient funds | Persisted as `FAILED` (no ledger rows, no balance change) | A retry with the same key returns the same `FAILED` result instead of re-evaluating against a different balance. |
| Invalid input / unknown wallet | Rejected, **nothing persisted** | Deterministic errors; retrying gives the same error. |
| Timestamps | `timestamptz`, exposed as ISO-8601 UTC | Unambiguous. |

### Lock order (deadlock freedom)
Every transfer transaction acquires locks in one fixed order: **(1)** advisory lock on the idempotency key (only if a key is given), **(2)** wallet rows sorted by id. A transaction only ever waits for a lock *later* in this order than the ones it holds, so a wait cycle is impossible.

## 3. API contract

### `POST /transfers`
```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```
* `idempotencyKey` optional (≤255 chars). Without it, duplicates cannot be detected.
* `amount`: JSON integer (`Number.isSafeInteger`), `1 ≤ amount ≤ 10^15`. Booleans/floats/strings/NaN rejected.
* `fromWalletId != toWalletId`.

| Status | When | Body |
|---|---|---|
| 201 | New transfer, `PROCESSED` | transfer object |
| 200 + `Idempotent-Replayed: true` | Key seen before, same payload, original was `PROCESSED` | the **original** transfer object |
| 422 | Transfer `FAILED` (insufficient funds) – first call *and* replays | transfer object (`status=FAILED`, `failureReason=INSUFFICIENT_FUNDS`) |
| 400 | Validation error | `{"error":{"code":"VALIDATION_ERROR","message":…}}` |
| 404 | Unknown wallet | `WALLET_NOT_FOUND` |
| 409 | Key reused with a *different* payload | `IDEMPOTENCY_KEY_CONFLICT` |
| 503 | Retryable DB condition (lock timeout, deadlock/serialization failure, connection loss) | `SERVICE_UNAVAILABLE` – nothing was committed, retry is safe |

Transfer object
```json
{ "id":"uuid", "idempotencyKey":"abc123", "fromWalletId":"wallet_1", "toWalletId":"wallet_2",
  "amount":100, "status":"PROCESSED", "failureReason":null,
  "createdAt":"2026-10-31T09:00:00.000Z", "updatedAt":"2026-10-31T09:00:00.000Z" }
```

### Supporting endpoints (to seed/inspect data; optional per the assignment)
* `POST /wallets` `{ "id": "wallet_1", "initialBalance": 500 }` → 201 / 409 if it exists / 400
* `GET /wallets/{id}` → `{ id, balance, createdAt }`
* `GET /wallets/{id}/ledger` → ledger entries for the wallet (oldest first)
* `GET /transfers/{id}` → transfer object (404 for unknown or malformed ids)

## 4. State machine

```
PENDING ──► PROCESSED
   └──────► FAILED          (PROCESSED and FAILED are terminal)
```
Enforced in three places: the domain model (`Transfer.transitionTo`), the repository (`UPDATE … WHERE id=$ AND status='PENDING'`, `rowCount` must be 1) and the database (trigger raises if `OLD.status <> 'PENDING'`). `PENDING` exists only *inside* the transfer transaction; a committed `PENDING` row is an integrity violation.

## 5. Idempotency behaviour

1. Take the advisory lock for the key, then look up `idempotency_records`.
2. **Found, same fingerprint** (`sha256(from,to,amount)`) → return the stored transfer (`replayed=true`), no side effects.
3. **Found, different fingerprint** → `409 IDEMPOTENCY_KEY_CONFLICT`.
4. **Not found** → execute and insert the record in the same transaction.

Duplicate side effects are prevented by (a) the advisory lock and (b) the `PRIMARY KEY`/`UNIQUE` constraints as backstop.

## 6. Failure modes & retry behaviour

| Failure | Outcome |
|---|---|
| Crash / error before commit | Whole transaction rolls back: no transfer, ledger, balance change or idempotency record. Client retries safely. |
| Crash after commit, response lost | Retry with the same key → original result. |
| Client timeout while the request still runs | Retry queues on the advisory lock, then sees the committed record and replays. |
| Two debits against the same wallet at once | Serialised by the row lock; the second sees the reduced balance and may become `FAILED`. Never negative. |
| Opposite transfers A→B and B→A | No deadlock (sorted lock order, §2). |
| Row lock wait > `lock_timeout` (10 s) | PostgreSQL error `55P03` → HTTP 503; nothing committed, safe to retry. |
| Database unreachable | Connection error → HTTP 503; idle-client errors are logged, not fatal. |
| Unexpected exception inside the transaction | `ROLLBACK`, then HTTP 500 with a generic message (details only in logs). |

## 7. Consistency expectations

Strong consistency: once a response returns, balances, ledger and transfer status are committed atomically. Invariants verified by `checkIntegrity()`:

1. `balance = initial_balance + Σcredits − Σdebits` per wallet
2. `Σ DEBIT == Σ CREDIT` globally
3. every `PROCESSED` transfer has exactly one DEBIT and one CREDIT of its amount; every `FAILED` one has none
4. no committed `PENDING` transfers
5. no wallet balance < 0

Ledger rows are immutable (row triggers block UPDATE/DELETE).

## 8. Schema highlights
`wallets(balance CHECK >= 0)`, `transfers(status CHECK, from<>to, FAILED ⇔ failure_reason)`, `ledger_entries(UNIQUE(transfer_id,type))`, `idempotency_records(key PK)`, indexes on `ledger_entries(wallet_id,id)` and `transfers(from|to_wallet_id, created_at)`. Created idempotently on start (`npm run migrate`); a real deployment would use a migration tool.

## 9. Observability
Structured `key=value` log lines through an injectable logger:
`event=transfer_processed|transfer_failed|transfer_replayed|transfer_rejected transfer_id=… idempotency_key=… from=… to=… amount=… status=… duration_ms=…`. Only ids and amounts are logged. Metrics are listed as future work.

## 10. Testing strategy
Behavioural tests (`node:test`) against a **real PostgreSQL** (no DB mocks); each test uses a throwaway schema so files can run in parallel:

* execution: balances, ledger pair, state; validation & unknown wallets persist nothing
* insufficient funds → `FAILED`, no ledger rows
* idempotency: replay, key reuse with a different payload, replay of `FAILED`, DB-level duplicate rejection
* rollback: injected failure after debit+credit leaves no trace; the retry succeeds
* DB guarantees: negative balance, terminal states, immutable ledger, one DEBIT/CREDIT per transfer
* concurrency (pool of separate connections): 30 parallel debits on one wallet, 20 identical idempotent requests, opposing transfers, multi-wallet ring
* HTTP mapping of every status code

## 11. Seeding
`npm run seed` creates `wallet_1` (500), `wallet_2` (0), `wallet_3` (1000) and two sample transfers with fixed idempotency keys (re-running is a no-op). `npm run seed:reset` truncates everything first (refused when `NODE_ENV=production`).
