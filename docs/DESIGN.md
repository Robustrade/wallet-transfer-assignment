# Wallet Transfer Service — Design

## 1. Goal

This service transfers money between two wallets. It uses Go, PostgreSQL, and GORM.

The main goals are:
- Do not allow a wallet to spend more money than it has.
- Do not process the same request twice when a client retries it.
- Update both wallets and the ledger in one database transaction.
- Keep a clear record of each successful transfer.

## 2. Main Parts

- **HTTP handlers:** Read requests, validate input, and return HTTP responses.
- **Service:** Applies transfer rules and manages the transfer flow.
- **Repository:** Reads and updates data in PostgreSQL.
- **Domain:** Holds the main models, errors, and money helpers.
- **Database migrations:** Create the required tables and constraints.

Keeping these parts separate makes the code easier to test and maintain.

## 3. Money

The service supports INR only. It stores money as integer paise, not as a floating-point number.

Go uses `int64`, and PostgreSQL uses `BIGINT`. The service checks that amounts are positive and guards against overflow.

## 4. Database Tables

- **users:** Stores user details.
- **wallets:** Stores each wallet's owner, currency, and current balance.
- **transfers:** Stores the transfer details and status.
- **ledger_entries:** Stores debit and credit entries for successful transfers.
- **idempotency_records:** Stores each idempotency key, request fingerprint, and saved response.

The database has constraints for rules such as non-negative wallet balances, positive transfer amounts, valid statuses, and unique idempotency keys.

## 5. Transfer Flow

The `POST /transfers` request follows this flow:

1. Validate the wallet IDs, amount, and idempotency key.
2. Start a PostgreSQL transaction.
3. Claim the idempotency key. If the key was used before, check the saved request fingerprint and response.
4. Lock the source and destination wallet rows in a consistent order.
5. Check that both wallets exist and that the source and destination are different.
6. Debit the source wallet only if it has enough money.
7. Check that adding the amount to the destination balance will not overflow.
8. Credit the destination wallet.
9. Add one debit ledger entry and one credit ledger entry for the same amount.
10. Mark the transfer as successful and save the response for the idempotency key.
11. Commit the transaction.

If a transfer is rejected for a business reason that must be replayed, such as insufficient funds, the service saves that response and commits it with the idempotency record. A retry with the same key then gets the same result. Unexpected database or system errors roll back the transaction, so partial balance or ledger updates are not committed.

## 6. Concurrency and Balance Safety

The source wallet is debited with a conditional update similar to:

```sql
UPDATE wallets
SET balance = balance - $1
WHERE id = $2 AND balance >= $1;
```

The balance check and debit happen in the database as one operation. This prevents two requests from spending the same funds.

The service locks both wallet rows in a consistent order. This lowers the risk of deadlocks when two transfers happen in opposite directions. The database also checks that a wallet balance cannot be negative.

Before crediting a wallet, the service checks for integer overflow. If the credit cannot be safely applied, the transfer must not partially complete.

## 7. Idempotency and Retries

Clients send an idempotency key with each transfer request.

- **Same key and same request:** Return the saved response. Do not transfer the money again.
- **Same key and different request:** Return `409 Conflict`.
- **Same key used by concurrent requests:** PostgreSQL's unique constraint decides which request claims the key. The other request waits for the result and then reads the saved outcome, or returns a safe error if the result cannot be read in time.
- **Saved business failure:** Return the same saved failure response on retry.
- **Unexpected failure before commit:** Roll back the transaction. The client can retry with the same key.

The request fingerprint is a SHA-256 hash of a canonical representation of the transfer fields. This avoids relying on an ambiguous string format.

## 8. Ledger Rules

For every successful transfer, the service writes:

- One `DEBIT` entry for the source wallet.
- One `CREDIT` entry for the destination wallet.
- The same positive amount in both entries.

Both entries and both balance changes are part of the same database transaction. Database constraints prevent more than one entry of each type for the same transfer. Tests also check that successful transfers have the expected pair of entries.

## 9. API Endpoints

- `POST /transfers` — Transfer money between wallets.
- `GET /wallets/{id}/balance` — Get a wallet balance.
- `GET /transfers/{id}` — Get transfer details.
- `GET /healthz` — Check service health.

The exact request fields and response formats are documented in `README.md` and must match the assignment requirements.

## 10. Testing

The test suite should cover normal requests and edge cases, including:

- Successful transfers and insufficient funds.
- Invalid amounts, missing wallets, and transfers to the same wallet.
- Repeated requests with the same key and requests that reuse a key with different data.
- Concurrent requests using the same key.
- Concurrent transfers that could spend the same balance.
- Transfers in opposite directions.
- Database failures and transaction rollback.
- Destination balance overflow and money-formatting limits.
- Invalid HTTP methods and oversized request bodies.

Unit tests cover service and handler behavior. Integration tests use PostgreSQL to check database constraints, transactions, and concurrency. A test is not considered passed until it has actually been run successfully.

## 11. Main Design Choices

- **Store the current balance:** This makes balance reads fast. The balance is updated in the same transaction as the ledger.
- **Use integer paise:** This avoids floating-point rounding errors.
- **Use PostgreSQL transactions:** This keeps balance changes, ledger entries, transfer status, and saved idempotency results consistent.
- **Store idempotency in the database:** This works across service restarts and multiple service instances.
- **Use versioned SQL migrations:** This gives the database schema one clear source of truth.

## 12. Limits and Follow-up Work

This service is an assignment implementation, not a complete banking platform. Before production use, it would need further review, monitoring, reconciliation, security checks, and load testing. The tests and build should be run in an environment with the Go version and PostgreSQL setup required by the project.
