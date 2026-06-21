# Wallet Transfer Service

A wallet-to-wallet transfer service built with **Java 21**, **Spring Boot 3.3**, and **PostgreSQL**. Implements idempotent transfers with double-entry ledger bookkeeping, pessimistic locking for concurrency control, and a state machine for transfer lifecycle.

## Tech Stack

- Java 21
- Spring Boot 3.3.5
- Spring Data JPA / Hibernate
- PostgreSQL 16
- Flyway (schema migrations)
- JUnit 5 + Testcontainers (integration tests against real PostgreSQL)
- Maven

## How to Run

### Prerequisites

- Java 21+
- Docker (for PostgreSQL via docker-compose and for tests via Testcontainers)

### Start PostgreSQL

```bash
docker-compose up -d
```

### Run the application

```bash
./mvnw spring-boot:run
```

The service starts on `http://localhost:8080`.

### Seed test wallets

The application does not auto-seed wallets. You can insert them directly:

```sql
INSERT INTO wallets (id, balance, version, created_at, updated_at)
VALUES ('wallet_1', 10000, 0, NOW(), NOW()),
       ('wallet_2', 5000, 0, NOW(), NOW());
```

## How to Test

```bash
./mvnw test
```

Tests use **Testcontainers** to spin up a real PostgreSQL instance. Docker must be running.

## API

### Create Transfer

```
POST /transfers
Content-Type: application/json

{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Returns `201 Created` with the transfer details. Duplicate requests with the same `idempotencyKey` return the original result without re-executing.

### Get Transfer

```
GET /transfers/{transferId}
```

### Get Wallet Balance

```
GET /wallets/{walletId}
```

## Design Decisions

### Database Schema

Four concerns, four tables:
- **wallets** — stores current balance with a `CHECK (balance >= 0)` constraint as a safety net. Uses `version` column for optimistic locking detection.
- **transfers** — the transfer record with a `UNIQUE` constraint on `idempotency_key`. Status enum: `PENDING -> PROCESSED | FAILED`.
- **ledger_entries** — double-entry bookkeeping. Every transfer produces exactly one DEBIT and one CREDIT entry.

Key constraints: non-negative balance, positive amounts, different source/destination wallets, valid status values.

### Idempotency Strategy

1. The `idempotency_key` column on `transfers` has a `UNIQUE` constraint.
2. Before executing, the service checks if a transfer with that key already exists.
3. If found, the existing transfer is returned immediately — no side effects are repeated.
4. The unique constraint also protects against race conditions where two requests with the same key arrive simultaneously — only one INSERT will succeed.

### Concurrency Strategy

**Pessimistic locking with `SELECT ... FOR UPDATE`** on wallet rows:

1. Both wallets are locked in **deterministic order** (sorted by ID) to prevent deadlocks.
2. The lock is held for the duration of the transaction (debit, credit, ledger write, status update).
3. Concurrent transfers targeting the same wallet serialize at the database level.
4. The `CHECK (balance >= 0)` constraint on the `wallets` table provides a database-level safety net against double spending.

### Why pessimistic over optimistic locking?

For a financial system where conflicts are expected (same wallet debited concurrently), pessimistic locking provides:
- Deterministic behavior under contention
- No retry loops needed at the application level
- Simpler reasoning about correctness

### State Machine

```
PENDING -> PROCESSED  (happy path)
PENDING -> FAILED     (insufficient funds or error)
```

Terminal states (PROCESSED, FAILED) cannot transition further. This is enforced in the domain model.

## Tradeoffs

- **Amounts stored as `BIGINT` (cents/minor units)** — avoids floating-point precision issues inherent to financial systems.
- **Single database transaction** — the entire transfer is atomic. Simpler than saga patterns but limits to single-DB deployments.
- **No wallet creation API** — wallets must be seeded directly. This keeps scope focused on the transfer logic.
- **Pessimistic locking reduces throughput** on hot wallets — acceptable for correctness-first design. A production system might use sharding or queue-based processing for high-volume wallets.
