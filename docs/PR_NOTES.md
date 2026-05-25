# PR Notes

## Database Schema

- `wallets` stores wallet IDs and current balances with a non-negative balance check.
- `transfers` stores source wallet, destination wallet, amount, and transfer status.
- `ledger_entries` stores immutable debit/credit records for processed transfers.
- `idempotency_records` stores the unique idempotency key and the transfer it produced.

## Transaction Strategy

Transfer creation runs in one PostgreSQL transaction. The service:

1. checks idempotency,
2. locks source and destination wallets with `FOR UPDATE`,
3. validates available balance,
4. creates the transfer in `PENDING`,
5. updates wallet balances,
6. writes debit and credit ledger entries,
7. marks the transfer `PROCESSED`,
8. saves the idempotency record,
9. commits.

Wallet locks are acquired in deterministic wallet ID order to reduce deadlock risk.

## Idempotency Strategy

`idempotency_records.idempotency_key` is the primary key. If a request repeats the same key after completion, the service returns the original transfer. If two concurrent requests race on the same key, the later duplicate-key insert is handled by rolling back that transaction and reading the already-created transfer.

## Concurrency Handling

The source and destination wallets are selected with `FOR UPDATE` before balance changes. This serializes concurrent transfers touching the same wallets and prevents double spending.

## Testing

Tests cover:

- request validation before opening a transaction,
- successful transfer balance updates,
- debit/credit ledger consistency,
- idempotent replay without duplicate ledger rows,
- insufficient funds rollback behavior,
- concurrent debits from the same wallet,
- concurrent requests with the same idempotency key.

## Assumptions And Tradeoffs

- Balances are stored on `wallets` instead of derived from ledger entries for simpler reads.
- Failed transfers are not persisted as `FAILED`; validation and insufficient funds currently roll back without a transfer row.
- Integration tests use PostgreSQL and are gated by `TEST_DATABASE_URL`.

## AI Usage Note

I used OpenAI Codex as a coding assistant to inspect the repository, discuss transaction/idempotency design, implement parts of the solution, review changes step by step, and add tests. I used it interactively rather than as a one-shot generator, validating behavior with manual API tests and `go test`.
