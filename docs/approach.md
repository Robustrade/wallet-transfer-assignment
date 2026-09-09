# Wallet Transfer Service — Approach

## 1. Understanding of the Problem

The assignment is to build a small backend service that supports wallet-to-wallet transfers.

The transfer operation needs to remain correct when requests are retried, duplicate requests are received, or multiple transfers execute concurrently.

The solution needs to guarantee:

- Idempotent request handling
- Correct wallet balances
- Double-entry ledger recording
- Atomic transfer execution
- Safe concurrent execution
- Safe transfer state transitions

The primary API will be:

POST /transfers

The implementation will use a layered architecture with clear separation between the handler, service, repository, and domain responsibilities.


## 2. API Contract

### Create Transfer

`POST /transfers`

The request will contain:

```json
{
  "idempotencyKey": "unique-request-key",
  "fromWalletId": "wallet-1",
  "toWalletId": "wallet-2",
  "amount": 100
}
```
The idempotencyKey identifies a logical client request and will be used to safely handle retries and duplicate requests.
The service will generate a unique transfer identifier for the transfer itself.
A successful request will return the transfer details, including its identifier and current status.
The API/service will validate:

Request validation:
- idempotency key is present and valid
- source and destination wallet IDs are present
- source and destination wallets are different
- amount is positive and uses a supported monetary precision

Business validation:
- source and destination wallets exist
- source wallet has sufficient balance

Invalid requests and business validation failures must not result in unintended wallet balance changes or successful-transfer ledger entries.

## 3. Data Model

The design will use three primary entities: wallets, transfers, and
ledger entries.

### Wallet

A wallet represents the current balance of an account.

Proposed fields:

- `id`
- `balance`
- `created_at`
- `updated_at`

### Transfer

A transfer represents a request to move funds between two wallets.

Proposed fields:

- `id`
- `idempotency_key`
- `from_wallet_id`
- `to_wallet_id`
- `amount`
- `status`
- `created_at`
- `updated_at`

The `idempotency_key` will have a uniqueness constraint to prevent
multiple transfers from being created for the same logical request.

### Ledger Entry

Each successful transfer will create exactly two ledger entries:

- one `DEBIT` entry for the source wallet
- one `CREDIT` entry for the destination wallet

Proposed fields:

- `id`
- `transfer_id`
- `wallet_id`
- `entry_type`
- `amount`
- `created_at`

The transfer and its two ledger entries will be created atomically
within the same database transaction.

Money values will use an exact representation rather than floating
point arithmetic.


## 4. Transfer Flow

A transfer is executed as a single atomic database transaction.

The high-level flow is:

1. Validate the incoming request.
2. Begin a database transaction.
3. Lock the source and destination wallet rows using `SELECT ... FOR UPDATE` in deterministic order based on wallet identifiers.
4. Validate that both wallets exist.
5. Attempt to create the transfer using the supplied idempotency key.
6. If the idempotency key already exists:
   - fetch the existing transfer;
   - verify that the request parameters match the original request;
   - return the existing transfer without applying the transfer again.
7. If the transfer is newly created, check that the source wallet has sufficient balance while the wallet rows are locked.
8. If the balance is insufficient, mark the transfer as `FAILED` and commit that state without changing wallet balances or creating successful-transfer ledger entries.
9. Debit the source wallet.
10. Credit the destination wallet.
11. Create one `DEBIT` ledger entry for the source wallet.
12. Create one `CREDIT` ledger entry for the destination wallet.
13. Mark the transfer as `PROCESSED`.
14. Commit the transaction.
15. Return the transfer result.

Wallets are locked in deterministic order based on their identifiers. This ensures that concurrent transfers involving the same wallets acquire locks in the same order and reduces the possibility of deadlocks.

The wallet locks are acquired before inserting the transfer record. This avoids lock-ordering issues caused by foreign-key checks on the `transfers` table while another transaction is attempting to lock the same wallet rows.

If any technical operation required for a successful transfer fails, the transaction is rolled back so that wallet balances, ledger entries, and transfer state are not left partially updated.

The transfer state follows:

`PENDING → PROCESSED`

or, when a business-level failure such as insufficient balance occurs:

`PENDING → FAILED`

## 5. Idempotency Strategy

The `idempotencyKey` identifies a logical transfer request and is stored with the transfer.

A unique database constraint is placed on `idempotency_key`. Idempotency is enforced using an atomic insert with conflict handling rather than a separate check-then-create operation.

For a new request:

1. Begin a database transaction.
2. Attempt to insert the transfer using the supplied `idempotencyKey`.
3. The database unique constraint on `idempotency_key` ensures that concurrent requests using the same key cannot create multiple transfer records.
4. If the insert succeeds, the request owns the newly created transfer and continues with wallet locking, balance validation, wallet updates, ledger creation, and final status update.
5. If the insert conflicts with an existing idempotency key, fetch the existing transfer.
6. If the existing transfer has the same source wallet, destination wallet, and amount, return the existing transfer result without applying the transfer again.
7. If the same idempotency key is reused with different transfer parameters, reject the request as an idempotency conflict.

The important concurrency property is that the unique database constraint and atomic insert provide the synchronization point. Two concurrent requests cannot both successfully insert a transfer with the same idempotency key.

A retry of an already completed transfer therefore returns the original transfer rather than modifying wallet balances or creating additional ledger entries.



## 6. Concurrency Strategy

The transfer operation must prevent double spending when multiple
transfers attempt to debit the same wallet concurrently.

The implementation will use a PostgreSQL transaction together with
row-level locking.

Before checking the source balance or modifying either wallet, the
source and destination wallet rows will be locked using
`SELECT ... FOR UPDATE`.

For example:

```sql
SELECT id, balance, created_at, updated_at
FROM wallets
WHERE id = $1
FOR UPDATE;
```

This ensures that concurrent transfers involving the same wallet are
serialized at the database row level.
Both wallet rows will be locked within the same transaction. Wallets
will be locked in a consistent deterministic order based on their
identifiers to reduce the possibility of deadlocks when two transfers
involve the same pair of wallets in opposite directions.
The balance check will happen after acquiring the lock. Therefore, a
second concurrent transfer cannot make a decision using a stale
balance.
The wallet updates, ledger entries, transfer state changes, and other
transfer side effects will occur within the same database transaction
and will be committed only after all operations succeed.

## 7. Transactions and Failure Handling

All state changes that constitute a successful transfer will be
performed within a single database transaction.

The transaction will include:

- transfer creation/state update
- source wallet debit
- destination wallet credit
- source `DEBIT` ledger entry
- destination `CREDIT` ledger entry
- final transition to `PROCESSED`

The transaction will be committed only after all required operations
succeed.

If a technical failure occurs during the transaction, the transaction
will be rolled back so that wallet balances, ledger entries, and
transfer state are not left partially updated.

Business validation failures, such as insufficient balance or invalid
wallets, will not result in any successful money movement or ledger
entries.

The implementation will distinguish between expected business
failures and unexpected technical/database failures. Unexpected
failures will cause the transaction to roll back and will be surfaced
through the appropriate API error response.

The intended state transition is:

`PENDING → PROCESSED`

for successful transfers, and:

`PENDING → FAILED`

for transfers that cannot be completed because of a business-level
failure.

## 8. Testing Strategy

The test suite will focus on the correctness guarantees of the transfer
workflow rather than only testing the happy path.

The following scenarios will be covered:

### Successful Transfer

Verify that:

- the source wallet is debited by the requested amount
- the destination wallet is credited by the requested amount
- the transfer is marked `PROCESSED`
- exactly two ledger entries are created
- one ledger entry is a `DEBIT` and the other is a `CREDIT`

### Insufficient Balance

Verify that a transfer is rejected when the source wallet does not
have sufficient funds and that no successful money movement or ledger
entries are created.

### Idempotency

Submit the same request multiple times using the same
`idempotencyKey` and verify that:

- only one transfer is created
- wallet balances are changed only once
- exactly two ledger entries are created
- retries return the existing transfer result

### Idempotency Key Conflict

Reuse an existing idempotency key with different transfer parameters
and verify that the request is rejected rather than modifying the
existing transfer.

### Transaction Rollback

Simulate a failure during the transfer workflow and verify that
partial wallet or ledger changes are not committed.

### Concurrent Transfers

Execute concurrent transfers against the same source wallet and
verify that the system prevents double spending. The final wallet
balance and successful transfer count must remain consistent with the
available funds.

Additional tests will cover invalid input, missing wallets, invalid
amounts, and other relevant validation failures.

## 9. Observability

The service should provide sufficient structured logging to diagnose
failed or unexpected transfers.

Logs should include identifiers such as:

- request identifier
- transfer identifier
- idempotency key where appropriate
- relevant wallet identifiers
- transfer status or failure reason

Important lifecycle events such as transfer initiation, successful
completion, and failure should be observable.

Sensitive information should not be unnecessarily written to logs.

Database and unexpected application errors should be logged with
sufficient context to support troubleshooting while avoiding exposure
of sensitive request data.

## 10. Trade-offs and Assumptions

### Database

PostgreSQL is preferred because the transfer workflow benefits from
ACID transactions, relational constraints, and row-level locking.

### Concurrency

Row-level locking with `SELECT ... FOR UPDATE` is chosen over
optimistic locking because the transfer operation requires a balance
check followed by a balance update, and explicit locking provides a
straightforward way to protect this critical section.

Optimistic locking could be considered if the system required higher
concurrency with low contention, but it would require handling version
conflicts and retries.

### Balance and Ledger

The wallet balance will be maintained as a current value for efficient
balance reads, while ledger entries will provide an audit trail of
money movements.

This means the implementation must ensure that balance changes and
ledger entries remain consistent within the same transaction.

### Money Representation

Floating-point arithmetic will not be used for monetary values.
Amounts will use an exact representation appropriate for financial
calculations.

### Architecture

A layered structure will separate HTTP handling, business logic,
persistence, and domain concerns. The goal is to keep the transfer
workflow easy to reason about and test without introducing unnecessary
complexity.

### Scope

The implementation will focus on the requirements of this assignment.
Features such as multi-currency support, distributed transaction
processing, external payment providers, event streaming, and complex
reconciliation are outside the current scope.