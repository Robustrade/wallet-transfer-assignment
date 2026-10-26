# Wallet Transfer — Understanding and Approach

## Problem understanding

The goal is to build a wallet-to-wallet transfer API that preserves financial correctness under concurrent requests, failures and retries.

The key requirements are atomic balance updates, double-entry ledger records, idempotent requests, and prevention of overspending.

## Proposed approach

- **Go and PostgreSQL:** Use a synchronous HTTP service backed by PostgreSQL transactions.
- **Data model:** Maintain wallets, transfers and ledger entries. Store wallet balances for efficient updates and preserve ledger entries for reconciliation.
- **Atomicity:** Execute balance changes, ledger postings and transfer-status updates inside a single database transaction.
- **Concurrency:** Lock both wallets in a consistent order and verify sufficient funds before debiting.
- **Idempotency:** Enforce unique idempotency keys in PostgreSQL and return the original committed result for identical retries.
- **Failure handling:** Commit insufficient-funds outcomes as FAILED without moving funds. Roll back database errors rather than persisting partial transfers.
- **Verification:** Use PostgreSQL integration tests for successful transfers, insufficient funds, rollback, idempotency and concurrent operations.

## Design trade-offs

The service will use a stored wallet balance alongside an immutable ledger. This introduces a reconciliation responsibility but avoids recalculating balances from ledger history for every transfer.

The initial scope excludes distributed messaging, external payment providers, and additional APIs not required by the assignment.