# Wallet Transfer Service — Design

## Problem Statement

Build a service that transfers money between two wallets and guarantees:

- **Idempotent request handling** — retrying the same request (same `idempotencyKey`) never applies the transfer twice, and always returns the original result.
- **Double-entry ledger** — every transfer produces exactly one `DEBIT` and one `CREDIT` ledger row for the same amount, so the ledger always balances and is independently auditable.
- **Correct balance tracking** — a wallet's balance only ever reflects transfers that actually completed; a failed transfer (e.g. insufficient funds) must leave both wallets untouched.
- **Safe concurrent execution** — many requests can hit the same wallet(s) at once (including two transfers moving money in opposite directions between the same pair of wallets, or the same idempotency key arriving twice simultaneously) without overdrawing a balance, losing an update, deadlocking, or double-processing.

## Database Design

Four tables, created by Flyway migration `V1__init_schema.sql`:

### `wallets`

| Column       | Type            | Notes                                   |
|--------------|-----------------|------------------------------------------|
| `id`         | VARCHAR(64) PK  |                                           |
| `balance`    | NUMERIC(19,2)   | `NUMERIC`, not `FLOAT`/`DOUBLE` — exact decimal storage for money, mapped to `BigDecimal` in `Wallet.balance`. Not constrained to be non-negative — fee/penalty-style debits are allowed to drive it negative; sufficiency is a decision made by the transfer workflow, not the schema. |
| `version`    | BIGINT          | Optimistic-lock column (defense in depth; see [Concurrency Handling](#concurrency-handling)). |
| `created_at` | TIMESTAMPTZ     |                                           |

### `transfers`

| Column             | Type            | Notes                                                                  |
|--------------------|-----------------|------------------------------------------------------------------------|
| `id`               | UUID PK         |                                                                        |
| `idempotency_key`  | VARCHAR(255)    | `UNIQUE` — the DB-level guard against concurrent duplicate requests.   |
| `from_wallet_id`   | VARCHAR(64) FK  | → `wallets.id`                                                         |
| `to_wallet_id`     | VARCHAR(64) FK  | → `wallets.id`                                                         |
| `amount`           | NUMERIC(19,2)   | `CHECK (amount > 0)`. `NUMERIC`, not `FLOAT`/`DOUBLE` — exact decimal storage for money, mapped to `BigDecimal` in `Transfer.amount`. |
| `status`           | VARCHAR(16)     | `CHECK IN ('PENDING','PROCESSED','FAILED')`                            |
| `failure_reason`   | VARCHAR(255)    | Set only when `status = FAILED`.                                       |
| `created_at`       | TIMESTAMPTZ     |                                                                        |
| `updated_at`       | TIMESTAMPTZ     |                                                                        |

Additional constraint: `CHECK (from_wallet_id <> to_wallet_id)`.
Indexes on `from_wallet_id`, `to_wallet_id` — `GET /wallets/{id}/transfers` filters on both to build a wallet's history without a full table scan.

### `ledger_entries`

| Column        | Type           | Notes                                   |
|---------------|----------------|-------------------------------------------|
| `id`          | UUID PK        |                                           |
| `transfer_id` | UUID FK        | → `transfers.id`                         |
| `wallet_id`   | VARCHAR(64) FK | → `wallets.id`                           |
| `entry_type`  | VARCHAR(8)     | `CHECK IN ('DEBIT','CREDIT')`            |
| `amount`      | NUMERIC(19,2)  | `CHECK (amount > 0)`. `NUMERIC`, mapped to `BigDecimal` in `LedgerEntry.amount`. |
| `created_at`  | TIMESTAMPTZ    |                                           |

Immutable once written — no update path exists. Every processed transfer writes exactly one `DEBIT` row (for `from_wallet_id`) and one `CREDIT` row (for `to_wallet_id`), both for the transfer's amount.
Index on `wallet_id` — backs per-wallet ledger lookups; index on `transfer_id` — backs fetching both entries of a given transfer (e.g. balance-checking a transfer's ledger pair).

### `idempotency_records`

| Column            | Type          | Notes                                  |
|-------------------|---------------|------------------------------------------|
| `idempotency_key` | VARCHAR(255) PK |                                        |
| `transfer_id`     | UUID FK       | → `transfers.id`                |
| `response_status` | INTEGER       | HTTP status the original request returned. |
| `response_body`   | TEXT          | Serialized `TransferResponse` JSON, replayed verbatim on retry. |
| `created_at`      | TIMESTAMPTZ   |                                           |

Written once, only after the transfer attempt has fully completed (processed or failed). This table is the *replay cache*; the actual concurrency guard against two simultaneous duplicate requests is `transfers.idempotency_key`'s unique constraint, not this table (see below).

## API Contract

### `POST /transfers`

Request:

```json
{
  "idempotencyKey": "string, required",
  "fromWalletId": "string, required",
  "toWalletId": "string, required",
  "amount": "positive decimal, required"
}
```

Response — `201 Created` if the transfer processed, `400 Bad Request` if it failed a business rule (e.g. insufficient funds); `status`/`failureReason` in the body carry the same outcome either way. The mapping lives on `TransferStatus.httpStatus()` and is applied consistently on both the first attempt and idempotent replays (the resolved status is persisted in `idempotency_records.response_status` and reused verbatim):

```json
{
  "transferId": "uuid",
  "idempotencyKey": "string",
  "fromWalletId": "string",
  "toWalletId": "string",
  "amount": "decimal",
  "status": "PENDING | PROCESSED | FAILED",
  "failureReason": "string | null",
  "createdAt": "instant"
}
```

Error responses (`{status, error, message, timestamp}`):

| Status | Cause                                                              |
|--------|----------------------------------------------------------------------|
| 400    | Bean validation failure (blank field, non-positive amount), or `fromWalletId == toWalletId`. |
| 404    | `fromWalletId` or `toWalletId` does not exist.                      |

Note: a `FAILED` transfer (e.g. insufficient funds) also returns `400`, but with a `TransferResponse` body (`{transferId, status: "FAILED", failureReason, ...}`), not the `{status, error, message, timestamp}` shape above — the two `400` cases are distinguishable only by response body shape, not status code.

### `GET /wallets/{walletId}`

Response — `200 OK`: `{ "id": "string", "balance": "decimal", "createdAt": "instant" }`
`404` if the wallet doesn't exist.

### `GET /wallets/{walletId}/transfers`

Response — `200 OK`: array of `TransferResponse`, both legs (sent and received) for the wallet, newest first.
`404` if the wallet doesn't exist.

## Flow



Step-by-step:

1. **Validate transport shape.** `TransferController` delegates to `@Valid` bean validation on `CreateTransferRequest` (`idempotencyKey`, `fromWalletId`, `toWalletId` non-blank; `amount` positive). Failures short-circuit with `400` before any service logic runs.
2. **Same-wallet check.** `TransferService.createTransfer` rejects `fromWalletId == toWalletId` with `400`.
3. **Idempotency fast path.** Look up `idempotency_records` by key. If found, deserialize and return the cached `TransferResponse` — the transfer is never re-attempted.
4. **Attempt the transfer, in one transaction.** If no cached record exists, `TransferAttemptExecutor.attemptTransfer` runs, `@Transactional`, covering everything from wallet existence checks through the final idempotency-record write:
   - Confirm both wallets exist with a plain unlocked read first, so a request against a missing wallet fails fast (`404`, nothing written) without ever taking a row lock.
   - Acquire `SELECT ... FOR UPDATE` row locks on both wallets, always in ascending wallet-id order, regardless of which is `from`/`to`.
   - Create the `Transfer` row with `status = PENDING` and `saveAndFlush` it immediately — this is the point the `idempotency_key` unique constraint is checked.
   - Check the (now-locked) `fromWallet` balance:
     - Insufficient → mark the transfer `FAILED` with reason `INSUFFICIENT_FUNDS`; no balance change, no ledger entries.
     - Sufficient → debit `fromWallet`, credit `toWallet`, add a ledger entry for both accounts — a `DEBIT` row for `fromWallet` and a `CREDIT` row for `toWallet`, both for the transfer's amount — and mark the transfer `PROCESSED`.
   - Persist an `idempotency_records` row caching the resulting `TransferResponse` (and HTTP status) for future replays.
   - The transaction commits (or fully rolls back) as a single unit.
5. **Concurrent-duplicate race.** If a second request with the same key committed first between step 3's check and step 4's insert, the `saveAndFlush` throws `DataIntegrityViolationException`. `TransferService` catches it, re-reads `idempotency_records` for that key (guaranteed visible now, since Postgres blocks the losing insert until the winning transaction finishes) and replays the winner's response.
6. **Response.** The controller returns the transfer's own `status`-derived HTTP code (`201` for `PROCESSED`, `422` for `FAILED`) with the `TransferResponse` body.

## Concurrency Handling

- **`@Transactional` attempt boundary.** The entire attempt — wallet locks, balance mutation, ledger writes, transfer state change, and the idempotency-record write — runs in one transaction on `TransferAttemptExecutor.attemptTransfer`, so it's all-or-nothing. This method lives on a separate Spring bean rather than a private method on `TransferService`, because calling an `@Transactional` method on `this` from within the same class bypasses Spring's proxy and silently runs with no transaction at all.
- **Row-level locking with `SELECT ... FOR UPDATE`.** `WalletRepository.findByIdForUpdate` uses `@Lock(LockModeType.PESSIMISTIC_WRITE)`. Both wallets involved in a transfer are locked for the duration of the transaction before their balances are read or mutated, so two concurrent transfers touching the same wallet(s) serialize on the lock instead of racing on a stale in-memory balance.
- **Deterministic lock ordering.** Locks are always acquired in ascending wallet-id order (whichever of `fromWalletId`/`toWalletId` sorts first, locked first), not `from`-then-`to`. This means two transfers moving money in opposite directions between the same pair of wallets can never deadlock on each other.
- **Two-layer idempotency guard.**
  1. *Fast path* — an application-level read of `idempotency_records` before attempting anything, which is cheap and handles the common case (sequential retries).
  2. *Race guard* — `transfers.idempotency_key`'s `UNIQUE` constraint at the database level. This is what makes idempotency airtight under true concurrency: an application-level check-then-insert alone has a race window between the check and the insert; two requests with the same key can both pass the fast-path check simultaneously. The unique constraint guarantees only one of them can ever successfully insert.
- **Losing the race is handled without double-processing.** When the insert loses the unique-constraint race, the whole attempt transaction rolls back (no partial wallet mutation is ever persisted), and the loser looks up and replays the winner's already-committed `idempotency_records` row in a fresh transaction. Reusing the same JPA persistence context after a flush failure is avoided deliberately — it's left in an undefined state per the JPA spec — so the replay lookup always runs in a new transaction/persistence context.

## Test Cases

### `WalletApiIntegrationTest` (real Postgres via Testcontainers)

- `GET /wallets/{id}` returns the current balance, reflects the effect of transfers, and returns `404` for an unknown wallet.
- `GET /wallets/{id}/transfers` includes both legs (sent + received), newest first, excludes transfers for unrelated wallets, and returns `404` for an unknown wallet.

### `TransferApiIntegrationTest` (real Postgres via Testcontainers)

- A valid transfer creates a `PROCESSED` transfer, debits/credits both wallets correctly, and writes exactly one balanced `DEBIT`/`CREDIT` ledger pair.
- The same `idempotencyKey` always replays the original response — both on an identical retry and on a retry with a different request body — and the transfer is applied exactly once (balance debited once, exactly 2 ledger entries, not 4 or 6).
- Insufficient funds produces a `400 Bad Request` response with a `FAILED` transfer, `failureReason = INSUFFICIENT_FUNDS`, no ledger entries, and the `fromWallet` balance untouched.
- Invalid requests (`fromWalletId == toWalletId` → `400`; unknown wallet → `404`) never create a `Transfer` row.

### `TransferServiceTest` (unit, mocked repositories)

- Covers the one path impractical to trigger deterministically over real concurrent HTTP requests: losing the `transfers.idempotency_key` insert race. Simulates a `DataIntegrityViolationException` on `saveAndFlush` and asserts the request replays the concurrent winner's response instead of re-applying the transfer.

### `ConcurrentTransferTest` (real Postgres via Testcontainers, genuine thread-pool concurrency)

- 20 concurrent transfers from the same wallet (distinct idempotency keys, released simultaneously via a `CountDownLatch`) never overdraw the source wallet and never lose an update — the final balance reflects exactly all 20 debits/credits, and each wallet ends up with exactly 20 ledger entries.
- 20 concurrent requests carrying the *same* idempotency key produce exactly one `Transfer` row and one identical response body across all callers — proving the unique-constraint race guard holds under real contention, not just in single-threaded tests.
