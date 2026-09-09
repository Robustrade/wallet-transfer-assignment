# Wallet Transfer Service — Design

## 1. Problem

Build a wallet-to-wallet transfer service that guarantees:

* Idempotent request handling
* Atomic transfers
* Correct wallet balances
* Double-entry ledger entries
* Safe concurrent execution
* Retry-safe state transitions

## 2. API

### POST /transfers

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

A successful transfer returns the transfer identifier and its processed state.

The same request with the same `idempotencyKey` returns the original result without creating another transfer or ledger entries.

Reusing an `idempotencyKey` with different request parameters returns a conflict.

## 3. Database Design

The service uses PostgreSQL with four primary tables:

* `wallets` — stores the current wallet balance.
* `transfers` — records transfer requests and their lifecycle state.
* `ledger_entries` — records the double-entry accounting entries.
* `idempotency_records` — stores request identity and the original result for safe retries.

Important database constraints:

* Primary keys on all entities.
* Foreign keys between related entities.
* Unique constraint on `transfers.idempotency_key`.
* Unique constraint on `idempotency_records.idempotency_key`.
* Positive transfer and ledger amounts.
* Source and destination wallets must be different.
* Transfer status is restricted to `PENDING`, `PROCESSED`, or `FAILED`.
* A transfer can have at most one `DEBIT` and one `CREDIT` ledger entry.

## 4. Transaction Strategy

A transfer is executed inside a single PostgreSQL transaction.

The transaction:

1. Handles the idempotency record.
2. Locks the source and destination wallet rows.
3. Validates the source balance.
4. Updates both wallet balances.
5. Creates the transfer and its two ledger entries.
6. Moves the transfer to `PROCESSED`.
7. Stores the result required for idempotent replay.
8. Commits the transaction.

Any failure causes the transaction to roll back.

Wallet rows are locked using PostgreSQL row-level locking (`SELECT ... FOR UPDATE`).

When two wallets are involved, they are always locked in deterministic ID order to reduce deadlock risk.

## 5. Idempotency

Idempotency keys are persisted in PostgreSQL rather than kept in application memory.

A unique database constraint prevents two concurrent requests from creating separate transfers for the same key.

The request parameters are represented by a request hash.

For a retry:

* Same idempotency key + same request → return the original result.
* Same idempotency key + different request → return a conflict.
* A committed transfer remains replayable after a process restart.

This prevents duplicate financial side effects.

## 6. Transfer State Machine

Allowed states:

```text
PENDING → PROCESSED
PENDING → FAILED
```

A processed or failed transfer cannot be processed again.

The state transition occurs within the transaction so that transfer state and financial side effects remain consistent.

## 7. Ledger Model

Each successful transfer creates exactly two ledger entries:

```text
DEBIT  source wallet      amount
CREDIT destination wallet amount
```

Both entries reference the same transfer.

The ledger provides the audit trail for the movement of funds.

## 8. Balance Consistency

The current wallet balance is stored in the `wallets` table for efficient reads.

Balance updates happen in the same transaction as the corresponding ledger entries.

Concurrent transfers cannot read and update the same wallet balance independently because the wallet row is locked before the balance is checked and modified.

## 9. Error Handling

The API distinguishes between:

* Invalid request
* Wallet not found
* Insufficient funds
* Idempotency conflict
* Internal/database failure

Financial state is never partially committed when an operation fails.

## 10. Testing Strategy

Tests will cover:

* Successful transfer
* Insufficient balance
* Wallet validation
* Idempotent retry
* Concurrent duplicate requests
* Idempotency key conflict
* Exactly two ledger entries per successful transfer
* Failed transfers producing no financial side effects
* Concurrent debits against the same wallet
* Final balance correctness under concurrent transfers

Concurrency tests will use multiple goroutines issuing transfers against the same wallet and verify that no double spending occurs.

## 11. Assumptions and Tradeoffs

* PostgreSQL is used because the assignment explicitly prefers it and its transactional and row-locking capabilities fit the consistency requirements.
* Wallet balances are stored rather than recalculated from the ledger for every request.
* `amount` is represented as an integer smallest currency unit to avoid floating-point precision issues.
* The transfer workflow is synchronous; asynchronous workers or messaging are unnecessary for the assignment scope.
* Authentication and authorization are outside the assignment scope.
