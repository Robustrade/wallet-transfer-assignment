# Wallet Transfer Service

A Spring Boot service for reliable wallet-to-wallet transfers. It uses PostgreSQL transactions, durable idempotency records, pessimistic wallet locks, and a database-enforced double-entry ledger.

## API

### Create a transfer

`POST /transfers`

```json
{
  "idempotencyKey": "transfer-2026-0001",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.00
}
```

Example successful response:

```json
{
  "transferId": "f8c717d6-193d-4f51-bd0f-8ee2d7a1ca46",
  "idempotencyKey": "transfer-2026-0001",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.0000,
  "status": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-08-30T12:00:00Z",
  "completedAt": "2026-08-30T12:00:00Z"
}
```

The amount must be positive with at most four decimal places, wallet IDs must be present, and source and destination wallets must differ.

## Transfer outcomes

| Outcome | Result |
| --- | --- |
| Sufficient funds | `PROCESSED`; balances and two ledger entries are committed. |
| Insufficient funds | `FAILED` with `INSUFFICIENT_FUNDS`; balances and ledger remain unchanged. |
| Unknown wallet | HTTP 404 with `WALLET_NOT_FOUND`. |
| Invalid request | HTTP 400 with validation details. |
| Reused key, different payload | HTTP 409 with `IDEMPOTENCY_CONFLICT`. |

## Idempotency

The `idempotencyKey` is the primary key of `idempotency_records`. Each record stores a SHA-256 fingerprint of the source wallet, destination wallet, and normalized amount.

For every request, the service obtains a PostgreSQL transaction-scoped advisory lock derived from the key before checking the record:

1. A matching existing record returns the original transfer response without new side effects.
2. A key reused with a different payload is rejected with HTTP 409.
3. A new record is written in the same transaction as the transfer.

This provides API-level exactly-once behavior for requests that supply an idempotency key, including a retry after a committed response is lost.

## Concurrency and transaction safety

Each transfer executes inside one Spring transaction. The transaction covers idempotency handling, transfer creation, wallet locking, balance changes, ledger creation, and the terminal transfer state.

Wallets are loaded with `SELECT ... FOR UPDATE`. The two wallet rows are acquired in lexical wallet-ID order, which serializes competing debits and avoids opposite-direction lock ordering. A competing transfer observes the source balance after the preceding transaction commits, preventing double spending.

## Ledger invariants

Each processed transfer has one debit from the source wallet and one credit to the destination wallet for the same amount.

Liquibase change set 3 adds PostgreSQL deferred constraint triggers that validate the final state at transaction commit:

- `PROCESSED` transfers require exactly one matching `DEBIT` and one matching `CREDIT` entry.
- Posting wallet IDs must match the transfer's source and destination wallets.
- Posting amounts must match the transfer amount.
- `PENDING` and `FAILED` transfers cannot have ledger entries.

Deferring the checks is important: Hibernate may flush the transfer, ledger rows, and status update in different order, but the database validates the fully assembled transaction before it commits.

## Transfer state machine

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

Terminal transfers cannot transition again. Only `PROCESSED` transfers move money and create ledger postings.

## Local development

Requirements: Java 17, Maven, Docker, and Docker Compose.

```bash
docker compose up -d
mvn spring-boot:run
```

The service starts on port `9090`. The local PostgreSQL container exposes port `5434`; Liquibase creates the schema and seeds `wallet_1` with `1000.0000` and `wallet_2` with `500.0000`.

## Tests

Run the full suite with Docker available:

```bash
mvn clean test
```

The integration tests use Testcontainers with PostgreSQL and cover successful transfers, idempotent replay and conflicts, insufficient funds, concurrent debits, concurrent duplicate requests, and invalid database-level ledger states.
