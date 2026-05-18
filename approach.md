# Implementation Approach

## Goal

Build a wallet transfer service that guarantees:

- atomic wallet-to-wallet transfers
- exactly-once API behavior when an `idempotencyKey` is provided
- correct balances under concurrent requests
- double-entry ledger consistency
- safe transfer state transitions

## High-Level Design

The implementation uses a layered structure:

- `handler`: HTTP validation and response mapping
- `service`: transfer orchestration and business rules
- `repository`: database access and transaction boundaries
- `domain`: entity validation and state transitions

This keeps transport concerns separate from workflow logic and persistence details.

## Data Model

The service uses four main tables:

- `wallets`: current balances
- `transfers`: one row per transfer request
- `ledger_entries`: immutable debit and credit records
- `idempotency_records`: durable request deduplication and response replay

Important constraints:

- non-negative wallet balances
- positive transfer amounts
- unique `idempotency_key`
- distinct source and destination wallets
- transfer status limited to `PENDING`, `PROCESSED`, `FAILED`
- unique `(transfer_id, type)` to guarantee one debit and one credit row per transfer

## Transfer Workflow

For `POST /transfers`, the service does the following inside one database transaction:

1. Validate the request.
2. Hash the logical request payload.
3. Try to create an idempotency record in `STARTED` state.
4. If the key already exists, lock the existing idempotency row and:
   - reject the request if the payload hash differs
   - replay the stored response if the original request completed
   - return conflict if the original request is still in progress
5. Insert the transfer row in `PENDING` state.
6. Lock both wallet rows with `SELECT ... FOR UPDATE`.
7. Debit the source wallet and credit the destination wallet.
8. Persist updated balances.
9. Insert one `DEBIT` and one `CREDIT` ledger entry.
10. Transition the transfer to `PROCESSED`.
11. Store the final response in the idempotency record and commit.

If debit fails because of insufficient funds:

1. Keep balances unchanged.
2. Do not insert ledger entries.
3. Transition the transfer to `FAILED`.
4. Persist the failure response in the idempotency record.

## Idempotency Strategy

Idempotency is database-backed rather than in-memory, so it survives retries and process restarts.

The key behaviors are:

- same key + same payload -> return original response
- same key + different payload -> `409 Conflict`
- same key while original request is unfinished -> `409 Conflict`
- side effects happen at most once because the stored response is written in the same transaction as the transfer outcome

This provides exactly-once semantics at the API layer for completed requests.

## Concurrency Strategy

The implementation uses Postgres row locks to prevent double spending.

- both wallet rows are locked before balances are updated
- wallet ids are sorted before locking to reduce deadlock risk
- transfer status, wallet updates, ledger writes, and idempotency completion all happen in the same transaction

This ensures that two concurrent debits against the same source wallet cannot both spend the same funds.

## State Management

Allowed transitions:

- `PENDING -> PROCESSED`
- `PENDING -> FAILED`

Repository updates only allow transitions from `PENDING`, which makes retries and duplicate handling safer.

## Failure Handling

The design explicitly considers:

- duplicate request delivery
- replay after client timeout
- insufficient funds
- partial execution risk
- concurrent requests against the same wallet

The system avoids partial side effects by making transfer creation, balance mutation, ledger writes, state transition, and idempotency completion part of one transaction.

## Observability

- request-level logging middleware
- explicit HTTP status and error responses

## Testing Strategy

The repo uses two levels of testing:

- fast service-level tests with an in-memory fake store for behavior coverage
- Postgres-backed integration tests for real idempotency replay and concurrent debit safety

The tests focus on:

- successful transfer execution
- replaying duplicate requests safely
- rejecting idempotency key reuse with a different payload
- failed transfers from insufficient funds
- preventing overspend under concurrent requests
- ensuring only successful transfers create ledger entries

## Tradeoffs

- Wallet balances are stored directly for efficient updates rather than derived from the ledger every time.
- The ledger is still written for auditability and balancing guarantees.
- The implementation is synchronous and does not include background recovery for abandoned `STARTED` idempotency rows.
