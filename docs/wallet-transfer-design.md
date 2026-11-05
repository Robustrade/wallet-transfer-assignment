# Wallet Transfer Service — Design

## 1. Problem statement and scope

Implement a synchronous `POST /transfers` use case that moves funds between two existing wallets. The service must provide retry-safe request handling, atomic balance updates, a double-entry ledger for successful transfers, correct balances under concurrency, and guarded transfer state transitions.

The design favors correctness and a small, understandable implementation over optional product features. Wallet creation/funding, balance/history read APIs, authentication, currency conversion, asynchronous processing, metrics, and tracing are outside the current scope.

## 2. Assumptions

1. PostgreSQL is the target database.
2. Each wallet has one currency. Transfers between different currencies are rejected; no currency conversion is performed.
3. Amounts are positive integers in the smallest supported currency unit. Floating-point arithmetic is not used.
4. Transfers are synchronous. The API responds after the database transaction commits.
5. A transfer from a wallet to itself is invalid.
6. Insufficient funds, currency mismatch, and destination-balance overflow are deterministic business failures. They are recorded as `FAILED` transfers and replayed for the same idempotency key.
7. Malformed requests and request-level validation failures are rejected before a transfer is created. Missing-wallet and infrastructure errors roll back the transaction, including the uncompleted idempotency claim.
8. Wallet creation and funding are setup concerns; tests seed wallets directly.

## 3. API contract

The endpoint is `POST /transfers`. The idempotency key is required and may be supplied in the `Idempotency-Key` header or the JSON `idempotencyKey` field. If both are supplied, they must match.

Example:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The amount is a positive integer in the wallet's smallest currency unit.

A successful new transfer returns `201 Created`:

```json
{
  "transfer": {
    "id": "transfer-id",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100,
    "status": "PROCESSED"
  }
}
```

A replay with the same key and same normalized request returns the same transfer result and status. Reusing a key with a different request returns `409 Conflict`.

Deterministic business failures return `422 Unprocessable Entity` and include the persisted `FAILED` transfer. Invalid input returns `400`; a missing wallet returns `404`; unexpected infrastructure errors return a generic `500` response without exposing internal details. See [http-api.md](http-api.md) for the complete mapping.

## 4. Data model

The schema contains four application tables:

- `wallets`: wallet ID, currency, stored non-negative integer balance, timestamps.
- `transfers`: source and destination wallet IDs, positive amount, status, optional failure code, timestamps.
- `ledger_entries`: transfer ID, wallet ID, entry type (`DEBIT` or `CREDIT`), positive amount, timestamp.
- `idempotency_records`: primary-key idempotency key, canonical request hash, transfer ID, stored response status/body, timestamp.

The database enforces primary/foreign keys, positive amounts, non-negative wallet balances, distinct transfer wallets, valid statuses/types, and at most one debit and one credit entry per transfer. The service validates that the two ledger entries form a matching pair for the transfer. Cross-row invariants are protected by the service transaction; they are not all independently enforced by SQL constraints.

Wallet balances are stored for efficient access and updated in the same transaction as ledger entries. Ledger entries are append-only through the repository API; corrections would require compensating entries rather than editing historical entries.

## 5. Transaction and concurrency strategy

One PostgreSQL transaction covers the idempotency claim, wallet locks, transfer row, balance changes, ledger entries, final response persistence, and commit.

1. Validate the idempotency key and request fields.
2. Begin a transaction and claim the key with `INSERT ... ON CONFLICT DO NOTHING`.
3. If the key already exists, load the existing record. A matching request hash replays the stored result; a different hash returns `409 Conflict`. Concurrent inserts serialize on the unique key.
4. Lock both wallet rows using `SELECT ... FOR UPDATE`, always in lexicographically sorted wallet-ID order.
5. Validate the wallets and create a `PENDING` transfer.
6. Check currency compatibility, available source funds, and destination balance overflow.
7. For a deterministic business failure, transition the transfer to `FAILED`, persist its replayable result, and commit without balance or ledger changes.
8. Otherwise debit the source, credit the destination, insert one `DEBIT` and one `CREDIT` ledger entry, and transition the transfer to `PROCESSED`.
9. Persist the response status/body in the idempotency record and commit.
10. Return the result only after commit succeeds.

The wallet row locks serialize competing transfers involving the same wallet. The non-negative balance constraint and conditional balance update provide additional protection. Deterministic lock ordering reduces deadlock risk for opposite-direction transfers.

A successful transfer's balance changes, ledger entries, terminal status, and idempotency response commit together or roll back together. A database error must not leave a partial transfer committed.

## 6. State transitions

Allowed transitions are:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

Repository updates include `WHERE status = 'PENDING'`, preventing terminal states from being changed by a repeated transition. `PENDING` is created and transitioned inside the same transaction; it is not a durable asynchronous/in-progress state visible after commit.

## 7. Idempotency and retry behavior

- The database primary key on `idempotency_key` is the deduplication authority.
- A SHA-256 hash is computed from canonical source wallet ID, destination wallet ID, and integer amount. The raw JSON representation is not hashed.
- A matching key and hash return the stored transfer result without repeating side effects.
- A matching key with a different hash returns `409 Conflict`.
- Successful results are stored with status `201`; deterministic business failures are stored with status `422`.
- The idempotency claim, transfer effects, and final response are committed in one transaction. If the transaction rolls back, the claim and effects roll back together.
- A client timeout after commit is safe: retrying with the same key returns the committed result.
- Idempotency records are not expired by this assignment implementation.

This is effectively-once processing within the PostgreSQL transaction and idempotency-key boundary. It is not a claim of universal exactly-once delivery across arbitrary distributed systems.

## 8. Failure modes and consistency expectations

| Scenario | Behavior |
| --- | --- |
| Malformed JSON, invalid key, invalid wallet ID, same wallet, or non-positive amount | Reject before transfer creation; no transfer side effects. |
| Unknown wallet | Roll back the transaction; return `404`; no committed idempotency result. |
| Insufficient funds | Persist `FAILED` transfer and replayable `422` result; no balance or ledger changes. |
| Currency mismatch or destination overflow | Persist `FAILED` transfer and replayable `422` result; no balance or ledger changes. |
| Same key, same payload | Return the original result; no new transfer or ledger entries. |
| Same key, different payload | Return `409`; preserve the original result. |
| Concurrent debits | Wallet row locks serialize updates; no negative balance or double spend. |
| Opposite-direction transfers | Sorted lock order reduces deadlock risk. |
| Failure before commit | Roll back all writes; retry with the same key can safely attempt the operation. |
| Response lost after commit | Retry returns the stored response. |

For every `PROCESSED` transfer, the source balance decreases and destination balance increases by the transfer amount, and exactly two equal-amount ledger entries exist: one debit from the source and one credit to the destination. A `FAILED` transfer changes neither balance and creates no ledger entries.

## 9. Observability expectations

The HTTP boundary emits structured logs for completed transfer attempts and unexpected failures. Logs should include outcome, HTTP status, transfer ID when available, source/destination wallet IDs, amount, failure code when applicable, elapsed time, and a SHA-256 hash of the idempotency key. Never log the raw idempotency key or credentials.

Expected business failures should be distinguishable from unexpected infrastructure failures. The service currently exposes generic internal errors to clients; server logs may include the underlying error for diagnosis. Metrics and distributed tracing are optional and are not implemented.

## 10. Testing strategy

The test suite includes domain, service, repository, handler, migration, application health, and opt-in PostgreSQL integration tests. PostgreSQL integration tests use a temporary isolated schema.

The integration suite covers successful transfer/replay/payload conflict, concurrent same-key requests executing once, competing debits not overdrawing a wallet, rollback after an injected ledger-write failure followed by a retry with the same idempotency key, and concurrent transfers in opposite directions. Unit tests cover domain validation, rollback orchestration, and guarded state transitions. The developer previously reported that the full suite, race-enabled suite, and `go vet` passed; the newly added logging and integration tests must be rerun locally before treating the updated branch as verified.

Further production hardening could include repository-level tests for invalid terminal-state updates, fault/timeout testing around database commit acknowledgement, and operational metrics/tracing. These are beyond the assignment's required scope.

## 11. Implementation sequence

1. Inspect the assignment and repository conventions.
2. Record the design and API contract.
3. Implement domain types and validation.
4. Add PostgreSQL schema and database setup.
5. Implement repositories and the atomic transfer service.
6. Add idempotency handling and HTTP exposure.
7. Add unit, integration, and concurrency tests.
8. Verify logs, migrations, health checks, and graceful shutdown.

This document describes the implemented synchronous design and identifies remaining hardening opportunities separately from current guarantees.
