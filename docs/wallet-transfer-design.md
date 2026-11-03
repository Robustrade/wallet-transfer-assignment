# Wallet Transfer Service — Design

## 1. Scope and goals

Implement the synchronous `POST /transfers` use case with:

- retry-safe request handling when an `idempotencyKey` is supplied;
- atomic movement of funds between two wallets;
- a double-entry ledger for every successful transfer;
- correct balances under concurrent requests;
- explicit, guarded transfer state transitions.

The implementation should favor correctness and a small, understandable design over optional product features. Balance/history read APIs, authentication, multi-currency support, asynchronous processing, and operational dashboards are out of scope unless the existing project or reviewer requires them.

## 2. Assumptions to confirm during implementation

1. PostgreSQL is the target database, as preferred by the assignment.
2. A wallet belongs to one currency. This assignment does not define currency conversion; transfers between wallets of different currencies are rejected if currency is represented in the schema.
3. Amounts are positive integers in the smallest supported currency unit. Floating-point arithmetic is not used for money.
4. A transfer is synchronous: the API returns after its database transaction commits.
5. A transfer from a wallet to itself is invalid.
6. Insufficient funds and other business-rule failures are deterministic results and are stored against the idempotency key. Malformed requests are rejected before a transfer is started.
7. Wallet creation and funding are setup concerns for this assignment. Tests need a reliable way to create wallets with initial balances.

These assumptions should be revisited if the starter project or interviewer provides a different contract.

## 3. API contract

### Request

`POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Validation:

- all fields are required;
- the idempotency key and wallet IDs must be non-empty and within documented length limits;
- amount must be greater than zero;
- source and destination wallets must differ;
- the request must not use floating-point or fractional amounts.

### Successful response

Return `201 Created` after commit. The response should contain a stable transfer identifier, source and destination wallet IDs, amount, state, and creation timestamp.

```json
{
  "transferId": "transfer_123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "createdAt": "2026-01-01T12:00:00Z"
}
```

A replay with the same key and same request payload returns the originally stored HTTP status and response body. It must not create another transfer or ledger entries.

Reusing a key with a different payload returns `409 Conflict` and does not execute the new request.

### Business failures

- `400 Bad Request`: malformed input or invalid amount.
- `404 Not Found`: source or destination wallet does not exist.
- `409 Conflict`: insufficient funds, same-wallet transfer, currency mismatch, or idempotency-key reuse with a different payload.
- `500 Internal Server Error`: unexpected persistence or infrastructure failure.

The exact error response shape will be standardized when the HTTP handler is implemented. A deterministic business failure associated with an accepted idempotency key is stored and replayed; an unexpected database failure rolls back the transaction so a retry can safely attempt the operation again.

## 4. Data model

Use four tables. Names and types can be adjusted to match the repository's conventions.

### `wallets`

- `id` — primary key.
- `currency` — currency code, if currency is represented.
- `balance` — non-negative integer amount in the smallest currency unit.
- `created_at`, `updated_at`.

The balance is stored for efficient reads and updated in the same transaction as the ledger. The ledger remains the audit trail.

### `transfers`

- `id` — primary key.
- `from_wallet_id`, `to_wallet_id` — foreign keys to wallets.
- `amount` — positive integer amount.
- `status` — constrained to `PENDING`, `PROCESSED`, or `FAILED`.
- `failure_code` — nullable machine-readable reason for a failed transfer.
- `created_at`, `updated_at`.

Add checks for positive amount and different source/destination wallet IDs. Index wallet IDs and creation time for future history queries if useful.

### `ledger_entries`

- `id` — primary key.
- `transfer_id` — foreign key to transfers.
- `wallet_id` — foreign key to wallets.
- `entry_type` — constrained to `DEBIT` or `CREDIT`.
- `amount` — positive integer amount.
- `created_at`.

Add a unique constraint on `(transfer_id, entry_type)`, so a transfer cannot have two debit entries or two credit entries. Successful transfers must have exactly one debit and one credit of equal amount. Ledger entries are append-only; corrections should use compensating entries rather than editing historical entries.

### `idempotency_records`

- `idempotency_key` — primary key or unique key.
- `request_hash` — hash of the canonical request fields.
- `transfer_id` — nullable foreign key if the request produced a transfer record.
- `response_status` — stored HTTP status.
- `response_body` — stored response payload.
- `created_at`.

The request hash covers the source wallet, destination wallet, and amount. The key is scoped to this service; if account or tenant scoping is introduced later, that scope must be part of the unique key.

## 5. Transaction and concurrency strategy

Use one PostgreSQL transaction for idempotency handling, wallet locking, transfer state, balance changes, ledger entries, and the stored response.

1. Validate the request shape before opening the transaction.
2. Start a transaction and attempt to insert the idempotency record using its unique key.
3. If the key already exists, read the existing record:
   - matching request hash: return its stored status and body without side effects;
   - different request hash: return `409 Conflict`.
   Concurrent inserts for the same key are serialized by the unique constraint. The losing request reads the committed record after the winning transaction finishes.
4. Lock both wallet rows using `SELECT ... FOR UPDATE`, always in deterministic wallet-ID order. Consistent ordering reduces deadlock risk when transfers run in opposite directions.
5. Verify both wallets exist, are eligible, and have compatible currencies if applicable.
6. Insert the transfer in `PENDING` state.
7. Check available funds while holding the source wallet lock:
   - insufficient funds: mark the transfer `FAILED`, store the deterministic failure response, and commit without balance or ledger changes;
   - sufficient funds: debit the source and credit the destination, insert one `DEBIT` and one `CREDIT` ledger entry, then mark the transfer `PROCESSED`.
8. Store the final response in the idempotency record and commit.
9. Return only after commit succeeds.

All writes for a successful transfer commit together or roll back together. A database error must never leave only one balance update or one ledger entry committed.

The wallet row locks serialize competing transfers that touch the same wallet. The non-negative balance check provides an additional database-level invariant. Locking both wallets in sorted order avoids the common A-to-B versus B-to-A lock-order deadlock.

The `PENDING` state is an internal state within the transaction for this synchronous workflow; it should not be visible as a committed intermediate state. State changes are limited to `PENDING -> PROCESSED` and `PENDING -> FAILED`. Terminal states cannot transition again.

## 6. Idempotency and retry behavior

- The database unique constraint is the authority for deduplication; an in-memory cache is not sufficient.
- Store a canonical request hash so the same key cannot silently represent two different operations.
- Store the final response status and body so a replay returns the original result.
- Keep the idempotency record and transfer effects in the same transaction. If the transaction rolls back, neither the transfer nor its idempotency result is committed.
- A client timeout after commit is safe: retrying the same key returns the committed result.
- Concurrent requests with the same key must produce at most one transfer.
- Idempotency-record retention is not specified by the assignment; records should not be expired during the assignment's evaluation period.

## 7. Failure modes and consistency expectations

| Scenario | Expected behavior |
| --- | --- |
| Invalid request | Reject before starting a transfer; no database side effects. |
| Unknown wallet | Return not found; no balance or ledger changes. |
| Insufficient funds | Persist a `FAILED` transfer and replayable failure result; no balance or ledger changes. |
| Same key, same payload | Return the stored original result; no new side effects. |
| Same key, different payload | Return conflict; preserve the original result. |
| Concurrent debits | Wallet row locks serialize updates; no negative balance or double spend. |
| Opposite-direction transfers | Lock wallets in the same sorted order to reduce deadlocks. |
| Failure before commit | Roll back all writes; a retry can safely execute. |
| Response lost after commit | Retry returns the stored response. |

For every `PROCESSED` transfer, the source balance decreases by the transfer amount, the destination balance increases by the same amount, and exactly two equal-and-opposite ledger entries exist. Failed transfers do not change balances or create successful-transfer ledger entries.

## 8. Observability

Use structured logs for transfer ID, idempotency-key hash (not the raw key), source/destination wallet IDs, outcome, failure code, and transaction errors. Do not log sensitive request data unnecessarily.

At minimum, make unexpected errors distinguishable from expected business failures. Metrics and tracing are optional; useful future metrics include transfer success/failure counts, idempotency replays, transaction failures, and transaction duration.

## 9. Testing strategy

Use behavioral tests and a real PostgreSQL instance for transaction and locking behavior where practical.

- successful transfer updates both balances and creates exactly two balanced ledger entries;
- invalid amount and same-wallet requests are rejected;
- insufficient funds produce a failed result without changing balances;
- duplicate requests return the original response and do not repeat side effects;
- same key with a different payload returns conflict;
- concurrent requests with the same key create only one transfer;
- concurrent debits cannot overspend a wallet;
- opposite-direction transfers preserve balances and do not leave partial ledger entries;
- injected database failures roll back all transfer effects;
- terminal transfer states cannot transition again.

Run the full test suite and the language-appropriate formatter, linter, and race/concurrency checks before submission.

## 10. Implementation sequence

1. Inspect the starter repository and confirm language, framework, and existing conventions.
2. Add domain types and validation.
3. Add PostgreSQL migrations and database setup.
4. Implement wallet, transfer, ledger, and idempotency persistence.
5. Implement the atomic transfer service.
6. Add idempotency replay and payload-conflict handling.
7. Expose `POST /transfers`.
8. Add integration and concurrency tests.
9. Complete operational documentation and the pull request explanation.

This document records the proposed design. Implementation details should be adjusted only when repository inspection or tests reveal a concrete reason, and the rationale should be documented.
