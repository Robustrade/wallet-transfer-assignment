# Wallet Transfer Service

A production-grade wallet transfer service built with **Java 21 + Spring Boot + PostgreSQL**.

Demonstrates exactly-once transfer semantics with idempotency, double-entry ledger recording,
and safe concurrent debit handling using PostgreSQL row-level locks.

---

## Stack

| Layer | Technology |
|-------|-----------|
| Language | Java 21 |
| Framework | Spring Boot 4 |
| Build | Maven (mvnw wrapper included) |
| Database | PostgreSQL 16 |
| Persistence | Spring JDBC (`JdbcTemplate`) |
| Migrations | Flyway |
| Tests | JUnit 5, AssertJ, Testcontainers |

---

## How to Run Locally

### Prerequisites

- Java 21+
- Docker (for PostgreSQL via Docker Compose, or use a local PostgreSQL instance)

### 1. Start PostgreSQL

```bash
docker run --rm -d \
  -e POSTGRES_DB=wallet_transfer \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:16-alpine
```

### 2. Start the Application

```bash
./mvnw spring-boot:run
```

The app starts on `http://localhost:8080`. Flyway runs migrations automatically on startup.

### 3. Seed Test Wallets

```bash
# Insert two wallets directly (wallet creation API is out of scope)
docker exec -i <postgres-container-id> psql -U postgres -d wallet_transfer <<'SQL'
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES ('wallet_1', 1000, now(), now()),
       ('wallet_2', 0,    now(), now());
SQL
```

---

## API

### `POST /transfers` — Create a transfer

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "abc123",
    "fromWalletId":  "wallet_1",
    "toWalletId":    "wallet_2",
    "amount":        250
  }' | jq
```

**Success response (201)**
```json
{
  "transferId": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
  "state": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 250
}
```

**Idempotent retry** — same body, same key → same response, no duplicate side effects.

**Payload mismatch (422)** — same key, different amount:
```json
{ "error": "IDEMPOTENCY_KEY_REUSE", "message": "Idempotency key was already used for a different request" }
```

**Insufficient funds** — returns 201 with `state: FAILED`:
```json
{ "transferId": "...", "state": "FAILED", "failureReason": "INSUFFICIENT_FUNDS", ... }
```

### `GET /transfers/{id}` — Get transfer state

```bash
curl -s http://localhost:8080/transfers/3fa85f64-5717-4562-b3fc-2c963f66afa6 | jq
```

### `GET /wallets/{id}` — Get wallet balance

```bash
curl -s http://localhost:8080/wallets/wallet_1 | jq
```

---

## How to Run Tests

```bash
./mvnw -B test
```

- **`TransferServiceTest`** — fast service-level tests using H2 in-memory database.
  Covers success, idempotency replay, payload mismatch, insufficient funds, validation,
  and missing wallet.

- **`ConcurrentTransferIntegrationTest`** — fires 8 concurrent transfer attempts against
  a real PostgreSQL container (started automatically by Testcontainers). Asserts the
  core money invariant: no double-spending, no negative balances, ledger consistency.

---

## Error Reference

| HTTP Status | Error code | Cause |
|-------------|-----------|-------|
| 400 | `BAD_REQUEST` | Missing fields, same-wallet, non-positive amount, malformed JSON |
| 404 | `WALLET_NOT_FOUND` | Source or destination wallet does not exist |
| 422 | `IDEMPOTENCY_KEY_REUSE` | Idempotency key reused with a different payload |
| 500 | `INTERNAL_ERROR` | Unexpected server error |

---

## AI Disclosure

**Tool used:** Antigravity (Google DeepMind)

**How I used it:**
- Used as a pair-programming assistant to plan the architecture before writing code
  (documentation-first approach matching the assignment requirements).
- Reviewed and confirmed every design decision interactively: idempotency strategy,
  lock ordering, ledger constraint, test coverage scope.
- Generated boilerplate (DTOs, exception handler, test scaffolding) under my direction,
  with each file reviewed before commit.
- I guided the tool on what to build and reviewed all output. I can explain every line.

**What I wrote / decided myself:**
- The overall correctness model (idempotency key + request hash + PENDING insert as lock)
- Lock ordering strategy (alphabetical wallet ID order to prevent deadlocks)
- Decision to keep insufficient-funds as `200 FAILED` rather than `409` (idempotency argument)
- Test assertions and concurrency test structure

A full transcript of the AI session is available on request.
