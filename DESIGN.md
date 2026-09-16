# Wallet Transfer Service — Design

## Problem Statement

Build a service that transfers money between two wallets and guarantees:

- **Idempotent request handling** — retrying the same request (same `idempotencyKey`) never applies the transfer twice, and always returns the original result.
- **Double-entry ledger** — every transfer produces exactly one `DEBIT` and one `CREDIT` ledger row for the same amount, so the ledger always balances and is independently auditable.
- **Correct balance tracking** — a wallet's balance only ever reflects transfers that actually completed; a failed transfer (e.g. insufficient funds) must leave both wallets untouched.
- **Safe concurrent execution** — many requests can hit the same wallet(s) at once (including two transfers moving money in opposite directions between the same pair of wallets, or the same idempotency key arriving twice simultaneously) without overdrawing a balance, losing an update, deadlocking, or double-processing.

## Database Design

Four tables, created by Flyway migration `V1__init.sql`.

### `wallets`

| Column       | Type            | Notes                                   |
|--------------|-----------------|------------------------------------------|
| `id`         | VARCHAR(64) PK  |                                           |
| `balance`    | NUMERIC(19,4)   | `NUMERIC`, not `FLOAT`/`DOUBLE` — exact decimal storage for money, mapped to `BigDecimal` in `Wallet.balance`. `CHECK (balance >= 0)` — a wallet is never allowed to go negative; this is the schema-level backstop behind the application-level check in `Wallet.debit()`. |
| `created_at` | TIMESTAMPTZ     | Set by `Wallet.onCreate()` (`@PrePersist`).|
| `updated_at` | TIMESTAMPTZ     | Refreshed by `Wallet.onUpdate()` (`@PreUpdate`) on every balance change. |

No optimistic-lock (`version`) column: concurrency is handled entirely with pessimistic `SELECT ... FOR UPDATE` locks (see [Concurrency Handling](#concurrency-handling)), so an extra optimistic layer would add complexity without adding protection here.

### `transfers`

| Column             | Type            | Notes                                                                  |
|--------------------|-----------------|------------------------------------------------------------------------|
| `id`               | UUID PK         | Generated in `Transfer.createPending()`, before persistence.          |
| `idempotency_key`  | VARCHAR(255)    | `UNIQUE` — the DB-level guard against concurrent duplicate requests.   |
| `from_wallet_id`   | VARCHAR(64) FK  | → `wallets.id`                                                         |
| `to_wallet_id`     | VARCHAR(64) FK  | → `wallets.id`                                                         |
| `amount`           | NUMERIC(19,4)   | `CHECK (amount > 0)`. Mapped to `BigDecimal` in `Transfer.amount`.     |
| `status`           | VARCHAR(16)     | `CHECK IN ('PENDING','PROCESSED','FAILED')`                            |
| `failure_reason`   | VARCHAR(512)    | Set only when `status = FAILED`.                                       |
| `created_at`       | TIMESTAMPTZ     | Set by `Transfer.onCreate()` (`@PrePersist`).                          |
| `updated_at`       | TIMESTAMPTZ     | Refreshed by `Transfer.onUpdate()` (`@PreUpdate`) on every state transition. |

Additional constraint: `CHECK (from_wallet_id <> to_wallet_id)`.
Indexes on `from_wallet_id` and `to_wallet_id` for per-wallet transfer history lookups.

### `ledger_entries`

| Column        | Type           | Notes                                   |
|---------------|----------------|-------------------------------------------|
| `id`          | UUID PK        | Generated in `LedgerEntry.debit()`/`credit()` factory methods.|
| `transfer_id` | UUID FK        | → `transfers.id`                         |
| `wallet_id`   | VARCHAR(64) FK | → `wallets.id`                           |
| `entry_type`  | VARCHAR(8)     | `CHECK IN ('DEBIT','CREDIT')`            |
| `amount`      | NUMERIC(19,4)  | `CHECK (amount > 0)`. Mapped to `BigDecimal` in `LedgerEntry.amount`. |
| `created_at`  | TIMESTAMPTZ    | Set by `LedgerEntry.onCreate()` (`@PrePersist`). |

Append-only — `LedgerEntry` exposes no setters and no update path exists anywhere in the application. Every processed transfer writes exactly one `DEBIT` row (for `from_wallet_id`) and one `CREDIT` row (for `to_wallet_id`), both for the transfer's amount, in `TransferService.executeTransfer`.
Index on `wallet_id` backs per-wallet ledger lookups (`GET /wallets/{id}/ledger`); index on `transfer_id` backs fetching both entries of a given transfer.

### `idempotency_records`

| Column            | Type            | Notes                                  |
|-------------------|-----------------|------------------------------------------|
| `idempotency_key` | VARCHAR(255) PK |                                        |
| `request_hash`    | VARCHAR(128)    | SHA-256 of `fromWalletId\|toWalletId\|amount`. Detects a key reused with a *different* payload. |
| `transfer_id`     | UUID FK         | → `transfers.id`                |
| `created_at`      | TIMESTAMPTZ     | Set by `IdempotencyRecord.onCreate()` (`@PrePersist`). |
| `updated_at`      | TIMESTAMPTZ     | Refreshed by `onUpdate()` (`@PreUpdate`). |

Unlike a response-caching table, this row does **not** store the serialized response body — it only stores a pointer (`transfer_id`) to the authoritative `Transfer` row. A replay re-reads the current `Transfer` and rebuilds the response from it, so there's only ever one source of truth for a transfer's outcome. Written as the *last* statement of the same transaction that decides `PROCESSED`/`FAILED` (see [Flow](#flow)), so "the transfer happened" and "the key is claimed" become visible to other transactions atomically.

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

Response — `201 Created` the first time a key is processed, `200 OK` if the same key is replayed. `status` can be `PROCESSED` or `FAILED` either way — a `FAILED` business outcome (e.g. insufficient funds) is still a successfully-handled request, so it does not get a `4xx`/`5xx` status. This choice matters for idempotency: a retry of a `FAILED` attempt must deterministically replay `200 OK` + `FAILED`, not surface as an error on one call and a success on the next.

```json
{
  "transferId": "uuid",
  "status": "PROCESSED | FAILED",
  "fromWalletId": "string",
  "toWalletId": "string",
  "amount": "decimal",
  "failureReason": "string | null",
  "replayed": "boolean"
}
```

Error responses (`GlobalExceptionHandler`, body shape `{error, message}`):

| Status | `error`                     | Cause                                                              |
|--------|------------------------------|---------------------------------------------------------------------|
| 400    | `VALIDATION_ERROR`           | Bean validation failure (blank field, non-positive amount).        |
| 400    | `INVALID_REQUEST`            | `fromWalletId == toWalletId`.                                       |
| 404    | `WALLET_NOT_FOUND`           | `fromWalletId` or `toWalletId` does not exist.                     |
| 409    | `IDEMPOTENCY_KEY_CONFLICT`   | Same `idempotencyKey` reused with a different request payload.     |

Validation and unknown-wallet failures never write an `idempotency_records` row — the key is not "burned," so a client can retry with a corrected payload under the same key (see [Flow](#flow)).

### `GET /transfers/{id}`

Response — `200 OK`: the same `TransferResponse` shape as above (`replayed` always `false` here). `404` (`NOT_FOUND`) if the transfer doesn't exist.

### `GET /wallets/{id}`

Response — `200 OK`: `{ "walletId": "string", "balance": "decimal" }`. `404` (`WALLET_NOT_FOUND`) if the wallet doesn't exist.

### `GET /wallets/{id}/ledger`

Response — `200 OK`: array of `{ "entryId": "uuid", "transferId": "uuid", "type": "DEBIT | CREDIT", "amount": "decimal" }`, newest first. `404` (`WALLET_NOT_FOUND`) if the wallet doesn't exist.

## Flow

Step-by-step, all inside `TransferService`:

1. **Validate transport shape.** `TransferController` delegates to `@Valid` bean validation on `CreateTransferRequest` (`idempotencyKey`, `fromWalletId`, `toWalletId` non-blank; `amount` positive). Failures short-circuit with `400` before the service is ever called.
2. **Fail fast on business-invalid input.** `createTransfer` rejects `fromWalletId == toWalletId` and non-positive `amount` before touching the database at all — no transaction, no locks, no idempotency key burned.
3. **Attempt the transfer, in one transaction** (`executeTransfer`, run via `TransactionTemplate`, covering everything from the idempotency fast-path check through the final idempotency-record write):
   - `SELECT ... FOR UPDATE` the `idempotency_records` row for this key. If it already exists (a sequential retry), validate the request hash matches and return the referenced `Transfer` as a replay — no wallet locking needed for this fast path.
   - Otherwise, lock both wallets with `SELECT ... FOR UPDATE`, always in ascending wallet-id order regardless of `from`/`to` direction (see [Concurrency Handling](#concurrency-handling)); a missing wallet throws `WalletNotFoundException` here, which rolls back the transaction and burns nothing.
   - Create the `Transfer` row (`status = PENDING`, not yet persisted).
   - Attempt `fromWallet.debit(amount)` then `toWallet.credit(amount)`:
     - **Insufficient balance** → catch `Wallet.InsufficientBalanceException`, call `transfer.markFailed(reason)`, save the `Transfer`, save the `idempotency_records` row pointing at it, return the `FAILED` response. No wallet or ledger rows are touched.
     - **Sufficient** → save both wallets, write one `DEBIT` ledger entry (`fromWallet`) and one `CREDIT` ledger entry (`toWallet`) for the same amount, call `transfer.markProcessed()`, save the `Transfer`, save the `idempotency_records` row, return the `PROCESSED` response.
   - The transaction commits (or fully rolls back) as a single unit.
4. **Concurrent-duplicate race.** If a second request with the *same, brand-new* key raced past step 3's initial lock check (which only protects *existing* rows — see [Concurrency Handling](#concurrency-handling)) and both attempt to insert the same `idempotency_records` row, the loser's `save()` throws `DataIntegrityViolationException` at commit time. `createTransfer` catches this specific exception around the `TransactionTemplate.execute(...)` call and runs a **second, independent, read-only transaction** that re-reads `idempotency_records` for that key and replays the winner's response. This is safe without polling because Postgres only surfaces the unique-violation to the loser *after* the winner has fully committed.
5. **Response.** The controller maps `replayed` to an HTTP status (`200` if true, `201` if false) and returns the `TransferResponse` body as-is.

## Concurrency Handling

- **Explicit transaction boundaries via `TransactionTemplate`, not `@Transactional`.** `createTransfer` needs to run its main attempt and (on a specific failure) a second, independent replay read as two separate transactions on two separate connections — annotating both as `@Transactional` methods on the same class wouldn't work, because calling one from another via `this` bypasses Spring's proxy and silently runs outside any new transaction. `TransactionTemplate` sidesteps that entirely by managing both transactions programmatically from the same method.
- **Row-level locking with `SELECT ... FOR UPDATE`.** `WalletRepository.findByIdForUpdate` uses `@Lock(LockModeType.PESSIMISTIC_WRITE)`. Both wallets involved in a transfer are locked for the duration of the transaction before their balances are read or mutated, so two concurrent transfers touching the same wallet(s) serialize on the lock instead of racing on a stale in-memory balance.
- **Deterministic lock ordering.** Locks are always acquired in ascending wallet-id order (whichever of `fromWalletId`/`toWalletId` sorts first, locked first), not `from`-then-`to`. This means two transfers moving money in opposite directions between the same pair of wallets can never deadlock on each other.
- **Two-layer idempotency guard.**
  1. *Fast path* — `IdempotencyRecordRepository.findByKeyForUpdate` locks and checks for an existing row before doing any wallet work. This correctly serializes retries *of a key that already exists*: a second request blocks on this lock until the first's transaction commits, then sees the row and replays it.
  2. *Race guard for brand-new keys* — `SELECT ... FOR UPDATE` can only lock a row that already exists, so it cannot by itself stop two concurrent requests that both see "no row yet" for a key nobody has used before; both would otherwise proceed to run the full transfer. The backstop is the `UNIQUE` constraint on `idempotency_records.idempotency_key` (and `transfers.idempotency_key`): only one of the racing transactions can actually commit that insert. Postgres resolves this by having every other transaction inserting the same key value block until the first one finishes, then either fail with a unique-violation (if the first committed) or succeed (if the first rolled back).
- **Losing the race is handled without double-processing.** When the insert loses the unique-constraint race, the whole attempt transaction rolls back (no partial wallet mutation is ever persisted, since Postgres rolls back *everything* in that transaction, including the earlier wallet debit/credit), and the loser looks up and replays the winner's already-committed `idempotency_records` row in a fresh transaction, per step 4 above.

## Test Cases

### `WalletDomainTest`, `TransferStateMachineTest` (unit, no database)

- `Wallet.debit`/`credit` update balance correctly; debiting past the balance throws `InsufficientBalanceException` and leaves the balance unchanged; non-positive debit/credit amounts are rejected.
- `Transfer.createPending` rejects same-wallet transfers and non-positive amounts.
- The `PENDING → PROCESSED` and `PENDING → FAILED` transitions succeed exactly once; calling `markProcessed()`/`markFailed()` again on a terminal transfer throws `IllegalStateTransitionException`.

### `TransferServiceIntegrationTest` (real Postgres, via `@SpringBootTest`)

- A valid transfer produces a `PROCESSED` transfer and correctly updates both wallet balances.
- Exactly two balanced ledger entries (one `DEBIT`, one `CREDIT`, equal amounts) are written per processed transfer.
- The same `idempotencyKey` sent twice sequentially returns the original result (`replayed = true` on the second call) without moving money twice.
- Reusing an `idempotencyKey` with a different payload throws `IdempotencyKeyReusedException`.
- Insufficient balance marks the transfer `FAILED` without moving money or writing ledger entries, and a retry under the same key replays the same `FAILED` outcome.
- A transfer to/from an unknown wallet is rejected and does **not** claim the idempotency key — retrying with a valid wallet under the same key is allowed to proceed normally.
- Self-transfers (`fromWalletId == toWalletId`) are rejected.

### `ConcurrentTransferTest` (real Postgres, genuine thread-pool concurrency)

- N concurrent transfers draining the same wallet (distinct idempotency keys, released simultaneously via a `CountDownLatch`) never overdraw the source wallet: the number of `PROCESSED` results matches exactly how many the starting balance can support, the rest fail cleanly, and the final wallet balances match the ledger exactly.
- N concurrent requests carrying the *same* idempotency key produce exactly one executed transfer (one distinct `transferId`, one `DEBIT`/`CREDIT` ledger pair) and exactly `N - 1` replayed responses — proving the unique-constraint race guard (see [Concurrency Handling](#concurrency-handling)) holds under real contention, not just in sequential tests.

Both integration and concurrency tests run against a real Postgres instance (configured via `application.yml`, pointing at a local install) rather than an in-memory database, because the behavior under test — `SELECT ... FOR UPDATE` semantics and unique-constraint conflict resolution under concurrent transactions — is Postgres-specific and would not be faithfully reproduced by a substitute.
