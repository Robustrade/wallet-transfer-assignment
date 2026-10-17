# Pull Request Notes — Wallet Transfer Service

## Summary

This change implements a wallet transfer service using Go, PostgreSQL, and GORM.

The service is designed to:
- Transfer money between wallets safely.
- Prevent wallet balances from becoming negative.
- Avoid processing the same transfer twice when a request is retried.
- Save a debit and credit ledger entry for each successful transfer.
- Roll back partial changes when an unexpected error occurs.

## Main Features

- `POST /transfers` — Create a wallet transfer.
- `GET /wallets/{id}/balance` — Read a wallet balance.
- `GET /transfers/{id}` — Read transfer details.
- `GET /healthz` — Check service health.
- PostgreSQL schema and versioned SQL migration.
- Docker Compose setup for local development.
- Unit tests and PostgreSQL integration tests.

## Data Model

- `users` stores user details.
- `wallets` stores wallet balances and currency.
- `transfers` stores transfer details and status.
- `ledger_entries` stores debit and credit entries.
- `idempotency_records` stores request keys and saved responses.

The database uses constraints to reject invalid amounts, negative balances, invalid statuses, and duplicate idempotency keys. A successful transfer should have one debit entry and one credit entry for the same amount.

## How a Transfer Works

1. Validate the request.
2. Start a database transaction and claim the idempotency key.
3. If the key already exists, compare the request fingerprint and return the saved result when it matches.
4. Lock both wallet rows in a consistent order.
5. Debit the source wallet only when it has enough money.
6. Check for overflow, then credit the destination wallet.
7. Write the two ledger entries and update the transfer status.
8. Save the response for future retries and commit the transaction.

A saved business failure, such as insufficient funds, is committed with its idempotency result so the same request gets the same response on retry. Unexpected system or database errors roll back the transaction.

## Concurrency and Idempotency

- The debit uses a database condition to check the balance and subtract money in one operation.
- Wallet rows are locked in a consistent order to lower deadlock risk.
- A database unique constraint protects idempotency keys across multiple service instances.
- A repeated key with different request data returns `409 Conflict`.
- A repeated key with the same request returns the saved result instead of processing the transfer again.

## Tests to Run

The tests should check successful transfers, insufficient funds, invalid requests, duplicate keys, key reuse with different data, concurrent transfers, opposing transfers, rollback, ledger entries, and integer overflow.

Run the project's documented test commands in an environment with the required Go version and PostgreSQL. Do not report tests as passing unless they have completed successfully.

## Review Checklist

- [ ] API fields and responses match the assignment.
- [ ] All balance changes and ledger writes use the same transaction.
- [ ] Every successful transfer has one debit and one credit entry of equal amount.
- [ ] Retried requests do not create another transfer.
- [ ] Concurrent requests cannot spend the same funds twice.
- [ ] Business failures are saved when they must be replayed.
- [ ] Unexpected failures roll back partial changes.
- [ ] Unit and integration tests have been run and reviewed.
