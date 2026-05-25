# Design Notes

## Goal

Build a small wallet transfer service with correct idempotency, concurrency control, double-entry ledger recording, and safe transfer state transitions.

## Initial Architecture

```text
cmd/server
  starts the application

internal/handler
  HTTP request/response mapping

internal/service
  business workflow and transaction orchestration

internal/repository
  SQL persistence methods

internal/domain
  core models, states, and validation

migrations
  database schema
```

## Transfer Flow

1. Receive `POST /transfers`.
2. Validate request.
3. Start a database transaction.
4. Check or create an idempotency record.
5. Lock the source and destination wallets in deterministic order.
6. Check source balance.
7. Create transfer in `PENDING`.
8. Update wallet balances.
9. Insert one debit and one credit ledger entry.
10. Mark transfer `PROCESSED`.
11. Store the response for idempotent replay.
12. Commit.

## Concurrency Strategy

The preferred implementation will use PostgreSQL row-level locks with `SELECT ... FOR UPDATE` inside a transaction.

Wallet rows should be locked in deterministic order by wallet ID to reduce deadlock risk.

## Idempotency Strategy

`idempotency_records` should have a unique constraint on `idempotency_key`.

The stored record should include enough information to return the original transfer result when the same key is retried.

## Ledger Rules

Every processed transfer must create exactly two ledger entries:

- one `DEBIT` for the source wallet
- one `CREDIT` for the destination wallet

The total debit amount must equal the total credit amount for each transfer.

## Planned Tests

- successful transfer
- insufficient funds
- duplicate idempotency key returns same result
- duplicate idempotency key does not create duplicate ledger entries
- concurrent debits from the same wallet do not overspend
- ledger entries balance for each processed transfer
