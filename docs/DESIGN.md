# Wallet Transfer Service — Design

Written before the implementation, as required by ASSIGNMENT.md.

## Problem statement

Move value from one wallet to another exactly once per `idempotencyKey`,
record a balanced double-entry ledger, and keep balances correct under
retries and concurrent debits.

## API contract

The assignment requires one endpoint:

```text
POST /transfers
```

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

| Condition | HTTP | Transfer | Ledger | Balances |
| --- | --- | --- | --- | --- |
| First success | `201` | `PROCESSED` | debit + credit | updated atomically |
| Replay, same key and payload | `200` | original row | unchanged | unchanged |
| Same key, different payload | `409` | none | none | unchanged |
| Insufficient funds | `422` | `FAILED` | none | unchanged |
| Unknown wallet | `404` | none | none | unchanged |
| Invalid body | `400` | none | none | unchanged |

`wallet_1` and `wallet_2` are seeded at process start so the assignment
example can be called without extra APIs.

## Side effects

One database transaction:

1. Claim the idempotency key (unique insert).
2. Lock both wallets in sorted id order.
3. Debit the source only if `balance >= amount`.
4. Credit the destination.
5. Insert the transfer as `PROCESSED`.
6. Insert exactly two ledger entries.

A rollback leaves no debit without a credit.

## State machine

```
PENDING -> PROCESSED
PENDING -> FAILED
```

`PENDING` is in-memory while the transaction is open. After commit the
row is `PROCESSED` or `FAILED`. Retries of a completed key only read.

## Idempotency

- Stored in `idempotency_records` (unique `idempotency_key`).
- Request hash is SHA-256 of `fromWalletId|toWalletId|amount`.
- Same key + hash returns the original transfer.
- Same key + different hash is `409`.
- Unique constraint handles concurrent duplicates.

## Concurrency

```sql
UPDATE wallets
SET balance = balance - ?, version = version + 1
WHERE id = ? AND balance >= ?;
```

Zero rows means insufficient funds. Wallets are locked in sorted id
order so `A→B` and `B→A` cannot deadlock.

## Persistence

SQLite (allowed by the assignment). Tables: `wallets`, `transfers`,
`ledger_entries`, `idempotency_records`.

## Testing

Tests hit a real SQLite database: transfer execution, replay, ledger
balance, insufficient funds, and concurrent debits.
