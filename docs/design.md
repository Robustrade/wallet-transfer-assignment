# Wallet Transfer Service Design

## Problem Statement

The service accepts wallet-to-wallet transfer requests and guarantees that each accepted request either produces one balanced double-entry ledger pair or a durable failed transfer result. A retry with the same idempotency key returns the original transfer result and never applies side effects twice.

## API Contract

- `POST /wallets` creates a wallet for local setup and tests.
- `GET /wallets/{id}` returns a wallet and its stored balance.
- `POST /transfers` executes a transfer request.
- `GET /transfers/{id}` returns a transfer and its ledger entries.

Amounts are integer minor units. The transfer endpoint requires:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

## Persistence Model

SQLite is used because the assignment accepts it and it keeps the solution self-contained. The schema has:

- `wallets`: current stored balance with a non-negative balance check.
- `transfers`: request details, state, request hash, and a unique `idempotency_key`.
- `ledger_entries`: exactly one `DEBIT` and one `CREDIT` per processed transfer, enforced by `(transfer_id, entry_type)` uniqueness and service-layer transactional insertion.

Transfer wallet IDs are request snapshots so missing-wallet failures can be stored and replayed. Ledger rows still use foreign keys to keep posted accounting entries attached to valid wallets and transfers.

## Idempotency And Retry Behavior

The service stores one transfer row per idempotency key. Inside the transfer transaction it first looks up the key:

- If the key exists with the same normalized request hash, it returns the stored transfer and ledger rows.
- If the key exists with a different request hash, it returns `409 idempotency_key_conflict`.
- If the key does not exist, it creates a `PENDING` transfer and moves it to `PROCESSED` or `FAILED` before commit.

This handles lost responses and duplicate delivery after process restarts because the idempotency decision is durable in the database.

## Consistency And Concurrency

The whole workflow runs in a `BEGIN IMMEDIATE` SQLite transaction. That gives the writer lock before reading and updating balances. The debit is also guarded by an atomic update:

```sql
UPDATE wallets
SET balance = balance - ?
WHERE id = ? AND balance >= ?;
```

If the guarded debit updates zero rows, the transfer is marked `FAILED` with `insufficient_funds`. This prevents read-then-write races and double spending under concurrent debits.

## Failure Modes

- Invalid request shape or invalid values return `400` and do not create a transfer.
- Reusing an idempotency key with different request parameters returns `409`.
- Missing source or destination wallets create a durable `FAILED` transfer result.
- Insufficient funds create a durable `FAILED` transfer result without ledger entries.
- Unexpected database errors roll back the transaction and return `500`.

## Observability

The server exposes `GET /health` and logs HTTP method, path, status, and duration for each request. Transfer responses include state and error codes so failed business outcomes are visible to clients.

## Testing Strategy

Tests cover successful transfers, duplicate idempotency-key replay, idempotency-key conflicts, insufficient-funds failures, and concurrent debits from the same source wallet.
