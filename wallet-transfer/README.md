# Wallet Transfer Service

Production-grade wallet transfer service built with Java 21, Spring Boot 3, and PostgreSQL.

## Stack

- **Java 21** + **Spring Boot 3.3**
- **Spring Data JPA** (Hibernate)
- **PostgreSQL 16** (Flyway migrations)
- **Gradle 8**
- **Testcontainers** (integration tests)

## Quick Start

### Prerequisites

- Java 21+
- Docker (for local PostgreSQL)

### Run locally

```bash
# Start PostgreSQL
docker-compose up -d

# Run the service
./gradlew bootRun
```

Service starts on `http://localhost:8080`.

### Run tests

```bash
# All tests (Testcontainers spins up PostgreSQL automatically)
./gradlew test

# With test report
./gradlew test && open build/reports/tests/test/index.html
```

## API

### POST /transfers

Create a transfer. Idempotent — same `idempotencyKey` returns the original result.

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "txn-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100.00
  }'
```

**Responses:**
- `201 Created` — new transfer processed
- `200 OK` — idempotent replay, original result returned
- `400 Bad Request` — validation error or self-transfer
- `404 Not Found` — wallet not found
- `422 Unprocessable Entity` — insufficient funds

### GET /transfers/{id}

Fetch a transfer by ID.

### GET /wallets/{id}

Fetch wallet balance.

### GET /wallets/{id}/ledger

Fetch ledger entries for a wallet.

## Seed Data

Flyway `V2__seed_wallets.sql` creates three wallets for local development:

| Wallet ID  | Balance   |
|------------|-----------|
| `wallet_1` | 1,000.00  |
| `wallet_2` | 500.00    |
| `wallet_3` | 250.00    |

## Design Decisions

### Idempotency

Unique index on `transfers.idempotency_key`. Phase 1 commits a PENDING record before processing (`REQUIRES_NEW` transaction). Concurrent duplicates race to insert — unique constraint ensures one wins, the loser re-fetches. Replays at any stage (PENDING, PROCESSED, FAILED) return the cached response.

### Concurrency

Pessimistic locking (`SELECT FOR UPDATE`) on wallet rows. Wallets always locked in ascending ID order to prevent deadlocks. `CHECK (balance >= 0)` at the DB level is a final safety guard. `@Version` on Wallet adds optimistic locking as a secondary defense.

### Transfer State Machine

```
PENDING → PROCESSED
PENDING → FAILED
```

Two-phase execution:
1. **Phase 1** (`REQUIRES_NEW`): commit PENDING transfer
2. **Phase 2** (`TRANSACTIONAL`): lock wallets, validate, update balances, write ledger, mark PROCESSED
3. **Phase 3** (`REQUIRES_NEW`): on business failure, mark FAILED (survives Phase 2 rollback)

### Double-Entry Ledger

Every transfer produces exactly two immutable ledger entries: one DEBIT from the source wallet, one CREDIT to the destination wallet. Both are created atomically in Phase 2. The ledger always balances.
