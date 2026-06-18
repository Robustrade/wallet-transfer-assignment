# Wallet Transfer Service

A reliable wallet-to-wallet transfer service built with Java 17, Spring Boot 3, PostgreSQL, and Flyway.

## Tech Stack
- Java 17
- Spring Boot 3.2
- PostgreSQL 15
- Flyway (schema migrations)
- Testcontainers (integration tests)

## Prerequisites
- Java 17
- Maven
- Docker

## Running Locally

Start PostgreSQL:
```bash
docker run --name wallet-postgres \
  -e POSTGRES_USER=wallet_user \
  -e POSTGRES_PASSWORD=wallet_pass \
  -e POSTGRES_DB=wallet_db \
  -e TZ=UTC -e PGTZ=UTC \
  -p 5432:5432 -d postgres:15
```

Run the app:
```bash
mvn spring-boot:run
```

## API

### Create Transfer
```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "unique-key-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }'
```

### Seeded Wallets
| Wallet | Balance |
|--------|---------|
| wallet_1 | 1000.00 |
| wallet_2 | 500.00 |

## Running Tests
```bash
mvn test
```
Tests use Testcontainers — Docker must be running.

## Design Decisions

### Idempotency
idempotency_key stored with UNIQUE constraint in DB.
Duplicate requests return the original response without re-executing.

### Concurrency
Pessimistic row-level locking (SELECT FOR UPDATE).
Wallets locked in sorted ID order to prevent deadlocks.

### Double-Entry Ledger
Every transfer creates exactly two ledger entries — DEBIT and CREDIT.
Ledger entries cannot exist without a valid transfer (FK constraint).