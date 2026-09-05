# Wallet Transfer Service

A wallet-to-wallet transfer service implemented in Go using Echo, Ent ORM, and PostgreSQL.

The service focuses on the correctness guarantees expected from a financial transfer workflow:

* Idempotent transfer creation
* Atomic balance updates
* Protection against concurrent double-spending
* Double-entry ledger consistency
* Safe transfer state transitions
* Transactional rollback on technical failures

## 1. Tech Stack

* Go
* Echo
* Ent ORM
* PostgreSQL
* pgx
* Docker Compose (use to run PostgreSQL locally)
* UUID identifiers
* Make

PostgreSQL is used for integration and concurrency testing because the implementation relies on row-level locking using `SELECT ... FOR UPDATE`.

## 2. Architecture

The application follows a layered architecture:

```text
HTTP Request
    |
    v
Handler
    |
    v
DTO -> Domain
    |
    v
Transfer Service
    |
    v
Repository Interfaces
    |
    v
Ent Repository Implementations
    |
    v
PostgreSQL
```

Responsibilities are separated as follows:

```text
internal/handler
    HTTP binding, validation, response/error mapping

internal/service
    Transfer orchestration and business rules

internal/domain
    Domain models, statuses, failure reasons, and errors

internal/repository
    Persistence abstractions and transaction interfaces

internal/repository/entrepo
    Ent/PostgreSQL repository implementations

ent/schema
    Database schema definitions
```

The service layer does not depend directly on Ent, allowing the transfer business logic to be tested independently of the database.

## 3. Database Model

### wallets

Stores the current wallet balance.

Important fields:

* `id` — UUID primary key
* `balance` — integer balance in the smallest currency unit
* `created_at`
* `updated_at`

Balances use `int64` rather than floating-point values to avoid floating-point precision problems in monetary calculations.

### transfers

Stores the transfer lifecycle and idempotency information.

Important fields:

* `id` — UUID primary key
* `idempotency_key` — unique
* `from_wallet_id` — foreign key to wallet
* `to_wallet_id` — foreign key to wallet
* `amount`
* `status`
* `failure_reason`
* `created_at`
* `updated_at`

Supported states are:

```text
PENDING
PROCESSED
FAILED
```

Valid transitions are:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

Repository updates are guarded so that a completed or failed transfer cannot subsequently transition to another state.

### ledger_entries

Represents the double-entry ledger.

Every successful transfer creates exactly two entries:

```text
DEBIT  -> source wallet
CREDIT -> destination wallet
```

Both entries have the same amount and transfer ID.

A composite uniqueness constraint prevents duplicate entries for the same transfer, wallet, and entry type.

## 4. Transfer Transaction

Transfer execution occurs within one PostgreSQL transaction:

```text
BEGIN
  |
  v
Lock source and destination wallets
  |
  v
Verify both wallets exist
  |
  v
Create PENDING transfer
  |
  v
Verify sufficient balance
  |
  +---- insufficient ----> FAILED
  |
  v
Debit source
  |
  v
Credit destination
  |
  v
Create DEBIT + CREDIT ledger entries
  |
  v
PENDING -> PROCESSED
  |
  v
COMMIT
```

Balance changes, ledger entries, and transfer-state changes therefore succeed or fail atomically.

If a technical failure occurs after balance modification, the complete transaction is rolled back.

## 5. Concurrency Strategy

The service prevents concurrent double-spending using PostgreSQL row-level locks.

Both participating wallets are selected using:

```text
SELECT ... FOR UPDATE
```

before evaluating the source balance.

Wallets are locked in deterministic ID order to reduce deadlock risk when concurrent transfers involve overlapping wallets.

The debit query additionally requires:

```text
balance >= amount
```

as a defensive database-level check.

For example, given:

```text
source balance = 100

concurrent transfer A = 80
concurrent transfer B = 80
```

one transaction obtains the wallet lock and succeeds.

After it commits:

```text
source balance = 20
```

The other transaction then sees the updated balance and fails with insufficient funds.

The concurrency integration test runs against PostgreSQL rather than SQLite because PostgreSQL row-lock semantics are part of the guarantee being tested.

## 6. Idempotency

Every `POST /transfers` request requires an `idempotencyKey`.

The database enforces:

```text
UNIQUE(idempotency_key)
```

### Same key, same request

The existing transfer is returned without performing another financial operation.

No additional balance update or ledger entry occurs.

### Same key, different request

If the same key is reused with a different source wallet, destination wallet, or amount, the service returns:

```text
409 Conflict
```

### Concurrent duplicate requests

An initial idempotency lookup provides the normal replay path, but correctness does not depend on that lookup.

Two concurrent requests can both initially observe that a key does not exist.

The PostgreSQL unique constraint therefore acts as the final authority.

If insertion encounters PostgreSQL unique violation `23505`, the transaction is rolled back. The committed transfer is then loaded through the non-transactional repository and processed through the normal replay/conflict logic.

This also avoids attempting additional queries from a PostgreSQL transaction that has entered an aborted state after a constraint violation.

## 7. Failure Handling

### Insufficient funds

When both wallets exist but the source balance is insufficient:

```text
PENDING -> FAILED
failure_reason = INSUFFICIENT_FUNDS
```

The failed transfer is committed.

No wallet balance or ledger entry changes.

A retry using the same idempotency key therefore reproduces the original failure rather than executing the transfer later if the wallet balance changes.

### Missing wallet

Wallet IDs are protected by foreign-key constraints.

Wallet existence is checked before creating the transfer.

If either wallet does not exist:

* `wallet not found` is returned
* no transfer is persisted
* no ledger entry is created
* no balance is changed

### Technical failure

Unexpected persistence/infrastructure failures roll back the entire transaction.

For example:

```text
Debit source
Credit destination
Ledger insert fails
        |
        v
ROLLBACK
```

Neither balance modification remains committed.

## 8. API

### Create Transfer

```text
POST /transfers
```

Example:

```json
{
  "idempotencyKey": "transfer-001",
  "fromWalletId": "11111111-1111-1111-1111-111111111111",
  "toWalletId": "22222222-2222-2222-2222-222222222222",
  "amount": 100
}
```

A successful new transfer returns `201 Created`.

```json
{
  "id": "<transfer-uuid>",
  "fromWalletId": "11111111-1111-1111-1111-111111111111",
  "toWalletId": "22222222-2222-2222-2222-222222222222",
  "amount": 100,
  "status": "PROCESSED"
}
```

An idempotent replay returns the same transfer without repeating its financial effects.

| Scenario                     |                 HTTP Status |
| ---------------------------- | --------------------------: |
| New successful transfer      |               `201 Created` |
| Successful idempotent replay |                    `200 OK` |
| Invalid request              |           `400 Bad Request` |
| Wallet not found             |             `404 Not Found` |
| Idempotency conflict         |              `409 Conflict` |
| Insufficient funds           |  `422 Unprocessable Entity` |
| Unexpected error             | `500 Internal Server Error` |

### Health Check

```text
GET /health
```

returns:

```json
{
  "status": "ok"
}
```

## 9. Running Locally

### Prerequisites

* Go
* Docker
* Docker Compose
* Make

Start PostgreSQL:

```bash
make db-up
```

Run the service:

```bash
make run
```

The service listens on:

```text
http://localhost:8080
```

Stop PostgreSQL:

```bash
make db-down
```

To completely reset the local PostgreSQL volume:

```bash
make db-reset
```

> `make db-reset` removes the local database volume and all data stored in it.

## 10. Development Commands

Available commands can be displayed with:

```bash
make help
```

Install/download dependencies:

```bash
make deps
```

Generate Ent code, including PostgreSQL row-lock support:

```bash
make generate
```

Format the code:

```bash
make fmt
```

Run `go mod tidy`:

```bash
make tidy
```

Run the unit tests:

```bash
make test-unit
```

Run the PostgreSQL integration tests:

```bash
make test-integration
```

Run the complete Go test suite:

```bash
make test
```

Run formatting and tests before committing:

```bash
make check
```

## 11. Testing Strategy

Unit tests exercise the transfer service independently from PostgreSQL and cover:

* successful transfer
* idempotent replay
* conflicting idempotency key
* insufficient funds
* replay of a failed transfer
* missing wallet
* rollback when ledger persistence fails

The transaction fake snapshots repository state and restores it on callback failure, allowing technical rollback behavior to be tested at the service level.

### PostgreSQL Concurrency Test

Concurrency correctness is additionally tested using real PostgreSQL.

The test starts two simultaneous transfers competing for the same source balance and verifies:

```text
two concurrent debit attempts
           |
           v
     PostgreSQL locks
           |
       +---+---+
       |       |
       v       v
   PROCESSED  FAILED
```

The assertions verify:

* exactly one transfer succeeds
* exactly one fails with insufficient funds
* the source wallet is never overdrawn
* destination balances reflect only one successful transfer
* only the successful transfer creates ledger entries
* exactly one transfer is `PROCESSED`
* exactly one transfer is `FAILED`

A dedicated test database should be used because the integration-test setup clears test records.

## 12. Assumptions

* Amounts are positive integer values expressed in the smallest currency unit.
* Wallet balances cannot become negative.
* A wallet cannot transfer funds to itself.
* An idempotency key identifies one logical transfer operation.
* The same idempotency key cannot represent different transfer parameters.
* Successful transfers are final within the scope of the assignment.
* Insufficient-funds failures are final for their idempotency key.
* PostgreSQL is authoritative for concurrency behavior.

## 13. Tradeoffs and Future Improvements

### Idempotency persistence

The current implementation uses the unique `transfers.idempotency_key`.

A dedicated idempotency table could later support:

* multiple idempotent operation types
* serialized HTTP response persistence
* request fingerprints
* expiration policies
* failures that cannot create a valid transfer record

### Database migrations

Ent schema creation is used for local development simplicity.

A production system should use explicit, version-controlled database migrations that are reviewed and deployed independently of application startup.

### Accounting model

The current service guarantees the required debit/credit pair through transaction logic and database uniqueness constraints.

A larger financial platform could introduce immutable journal batches, account/currency dimensions, reconciliation processes, and stronger database-level accounting invariants.

### Observability

Production extensions could include:

* structured logs
* correlation IDs
* transfer latency metrics
* success/failure counters
* database lock-wait metrics
* distributed tracing

## 14. AI Assistance

AI assistance was used during implementation as permitted by the assignment.

It was used to:

* discuss architecture and transaction boundaries
* review PostgreSQL locking and concurrency behavior
* review idempotency race conditions
* identify failure and foreign-key edge cases
* review testing strategies
* assist with documentation

Suggestions were applied incrementally and validated through compilation, unit tests, PostgreSQL integration tests, and manual API/database verification.

The implementation decisions, guarantees, and tradeoffs are intended to remain independently explainable during code review.

Representative prompts or the development transcript can be provided with the submission as required.
