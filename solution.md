# Wallet Transfer Service Solution

## Overview

This service provides wallet creation, wallet lookup, wallet statements, transaction listing, and wallet-to-wallet transfers over HTTP. It uses Go, SQLite, `database/sql`, and sqlc-generated database accessors.

## Architecture

The code is organized into four practical layers:

- `router/`: HTTP routing, JSON decoding, validation, and response encoding.
- `controller/`: application orchestration and mapping between API models and database models.
- `datasource/`: database access, transaction workflow, idempotency, and transfer processing.
- `mapper/` and `models/`: API/domain representations and conversion helpers.

The database schema is applied at startup from `db/migrations/001_init.sql`. The SQLite database is stored in `wallet.db`.

## Transfer Workflow

A transfer follows this lifecycle:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

1. Begin a transaction and look up the supplied idempotency key.
2. If the key already exists, return its stored transaction ID without repeating side effects.
3. Create a new `PENDING` transfer and persist the idempotency record in the same transaction.
4. Commit the transfer record.
5. Process the transfer in a database transaction:
   - load the source and destination wallets;
   - atomically debit the source wallet only when sufficient funds exist;
   - credit the destination wallet;
   - create one `DEBIT` ledger entry and one `CREDIT` ledger entry;
   - transition the transfer from `PENDING` to `PROCESSED`.
6. If processing fails after the initial commit, transition the transfer to `FAILED` and store the failure reason in `comments`.

The unique primary key on `idempotency_record.idempotency_key` prevents duplicate idempotency records. The lookup-before-processing path makes retries return the original transaction ID.

Migration `002_ledger_entry_indexes.sql` adds indexes on `(wallet_id, created_at)` and
`created_at` to support wallet-scoped statements and global ledger reads ordered by time.

## Balance and Concurrency Strategy

Wallet balances are stored on the `wallet` table. Debits use a guarded SQL update:

```sql
UPDATE wallet
SET balance = balance - ?
WHERE wallet_id = ?
  AND balance >= ?;
```

The affected-row count is checked before continuing. This prevents a transfer from debiting an insufficient balance and keeps the debit, credit, ledger inserts, and status transition in one database transaction.

SQLite serializes conflicting writes. The test suite also exercises concurrent debits against the same source wallet and verifies that only one transfer succeeds when the available balance is insufficient for both.

## Double-Entry Ledger

Every successfully processed transfer creates exactly two ledger entries with the same transaction ID:

- a `DEBIT` entry for the source wallet;
- a `CREDIT` entry for the destination wallet.

Ledger inserts occur in the same transaction as the balance updates and status transition. If any ledger or status operation fails, the processing transaction rolls back.

## API Endpoints

- `POST /wallets`: create a wallet.
- `GET /wallets`: list wallets.
- `GET /wallets/{id}`: get one wallet.
- `GET /wallets/{id}/statements`: list ledger statements for a wallet.
- `GET /transactions`: list transfers.
- `POST /transfers`: create an idempotent transfer.

The complete OpenAPI contract is in [swagger.yaml](swagger.yaml).

### Transfer request

```json
{
  "idempotencyKey": "request-123",
  "fromWalletId": "wallet-1",
  "toWalletId": "wallet-2",
  "amount": 2500
}
```

Amounts are integer values in the smallest currency denomination. Wallet balances and transfer amounts must be greater than zero where applicable.

## Failure Handling

Invalid JSON and validation failures return `400`. Unsupported methods return `405`. Controller or database failures return `500`. A processing failure is persisted as a `FAILED` transfer when the failure-state update itself succeeds.

## Testing

The repository includes tests for:

- idempotent transfer retries;
- wallet and transaction data access;
- balanced debit and credit ledger entries;
- invalid transfer states;
- insufficient funds;
- concurrent debits;
- failed transfer and failure-state branches;
- controller mapping and error propagation;
- HTTP routing, validation, and response behavior.

Verification commands:

```bash
go test ./...
go test -race ./...
```
