# Robustrade Wallet Transfer Service

A reliable, transactional wallet-to-wallet transfer backend service built with **Go**, **PostgreSQL**, and **GORM**.

The service provides **exactly-once execution semantics at the API level** through durable idempotency, prevents double-spending under high concurrency, and maintains double-entry accounting invariants.

---

## Table of Contents

1. [System Overview](#system-overview)
2. [Architecture](#architecture)
3. [Money Representation](#money-representation)
4. [Database Design](#database-design)
5. [Transaction & Concurrency Strategy](#transaction--concurrency-strategy)
6. [Idempotency Policy](#idempotency-policy)
7. [API Contract & Examples](#api-contract--examples)
8. [Setup & Local Development](#setup--local-development)
9. [Running Migrations & Seed Data](#running-migrations--seed-data)
10. [Running Tests](#running-tests)
11. [Known Limitations & Trade-offs](#known-limitations--trade-offs)
12. [Repository Structure](#repository-structure)

---

## 1. System Overview

The Robustrade Wallet Transfer Service provides a reliable financial transfer engine supporting:
- **Durable Idempotency**: Replaying a transfer request with the same `idempotencyKey` returns the original result without triggering duplicate transfers or ledger writes.
- **Double-Entry Ledger Bookkeeping**: Every successful transfer generates an audit-ready, balanced pair of ledger records (1 DEBIT and 1 CREDIT) of equal amount.
- **Concurrency & Double-Spend Protection**: Uses atomic conditional updates (`WHERE balance >= amount`) and row-level locking to guarantee balances never become negative, even under simultaneous requests.
- **Deadlock Free Execution**: Locks participating wallets in deterministic primary-key order (`ORDER BY id ASC`).
- **Clean Layered Architecture**: Clear separation of concerns across HTTP Handlers, Service Logic, Repository/Persistence, Domain Entities, and Configuration.

---

## 2. Architecture

The codebase follows Clean Layered Architecture:

```text
wallet-transfer-assignment/
├── cmd/
│   └── api/
│       └── main.go                  # Application entry point, routing & graceful shutdown
├── internal/
│   ├── domain/                      # Core entities, money helpers, status & typed errors
│   │   ├── models.go
│   │   ├── errors.go
│   │   └── money.go
│   ├── config/                      # Environment-based configuration loader
│   │   └── config.go
│   ├── repository/                  # GORM database adapters, SQL queries & transactions
│   │   ├── wallet_repository.go
│   │   └── db.go
│   ├── service/                     # Transfer business logic, idempotency orchestration
│   │   └── transfer_service.go
│   └── handler/                     # HTTP boundary decoding, validation & status mapping
│       ├── transfer_handler.go
│       ├── wallet_handler.go
│       ├── health_handler.go
│       └── response.go
├── migrations/
│   └── 001_create_wallet_tables.sql # Production-ready PostgreSQL schema
├── scripts/
│   └── seed.sql                     # Seed data for development and manual testing
├── tests/
│   ├── unit/                        # Fast isolated unit tests with mock repository
│   │   ├── domain_test.go
│   │   ├── handler_test.go
│   │   └── transfer_service_test.go
│   └── integration/                 # Concurrency & PostgreSQL integration tests
│       └── transfer_integration_test.go
├── docs/
│   ├── DESIGN.md                    # Deep architectural design rationale
│   ├── PR_NOTES.md                  # PR summary, trade-offs, and checklist
│   └── AI_USAGE.md                  # AI usage disclosure
├── docker-compose.yml               # Local PostgreSQL 16 container definition
├── Makefile                         # Common build, test, and run tasks
├── .env.example                     # Sample environment variable configuration
├── go.mod
├── go.sum
└── README.md
```

### Layer Responsibilities:
- **Handler**: HTTP decoding, strict payload validation (`DisallowUnknownFields`), boundary constraints, and HTTP status code mappings. Handlers contain no business or persistence logic.
- **Service**: Enforces transfer business rules, calculates deterministic request hashes, coordinates transactions, and handles idempotency replays and failures.
- **Repository**: Executes GORM and parameterized SQL queries, acquires row locks, and manages database transactions.
- **Domain**: Pure models, error types, and monetary validation rules with zero dependencies on external frameworks.
- **Config**: Loads environment variables with robust defaults and validation.

---

## 3. Money Representation

- **Currency Supported**: Strictly **Indian Rupee (INR)**.
- **Storage Unit**: All monetary values are represented as integer **paise** ($1\text{ INR} = 100\text{ paise}$) using PostgreSQL `BIGINT` and Go `int64`.
  - ₹1.00 = `100` paise
  - ₹50.00 = `5000` paise
  - ₹100.50 = `10050` paise
- **No Floating-Point Math**: IEEE 754 floating-point (`float32`/`float64`) is strictly forbidden for money calculations to eliminate binary rounding discrepancies.
- **Range & Overflow Protection**: Validations ensure amounts are strictly positive ($> 0$) and guarded against integer overflow.

---

## 4. Database Design

```mermaid
erDiagram
    users ||--o{ wallets : owns
    wallets ||--o{ transfers : "from / to"
    transfers ||--|{ ledger_entries : generates
    transfers ||--o| idempotency_records : "claimed by"

    users {
        varchar id PK
        varchar name
        timestamptz created_at
    }

    wallets {
        varchar id PK
        varchar user_id FK
        bigint balance
        varchar currency
        timestamptz created_at
        timestamptz updated_at
    }

    transfers {
        varchar id PK
        varchar from_wallet_id FK
        varchar to_wallet_id FK
        bigint amount
        varchar currency
        varchar status
        varchar failure_code
        timestamptz created_at
        timestamptz updated_at
    }

    ledger_entries {
        bigserial id PK
        varchar transfer_id FK
        varchar wallet_id FK
        varchar entry_type
        bigint amount
        timestamptz created_at
    }

    idempotency_records {
        bigserial id PK
        varchar idempotency_key UK
        varchar request_hash
        varchar transfer_id FK
        int response_status
        text response_body
        timestamptz created_at
        timestamptz completed_at
    }
```

### Key Invariants & Constraints:
- `wallets.balance >= 0`: Database check constraint preventing negative balances.
- `transfers.amount > 0`: Transferred amount must be positive.
- `transfers.from_wallet_id <> transfers.to_wallet_id`: Sender and receiver wallets must differ.
- `transfers.status IN ('PENDING', 'PROCESSED', 'FAILED')`: Enforces the state machine.
- `UNIQUE (transfer_id, entry_type)`: Ensures at most one `DEBIT` and one `CREDIT` record per transfer.
- `UNIQUE (idempotency_key)`: Guarantees unique request arbitration across concurrent threads and nodes.

---

## 5. Transaction & Concurrency Strategy

### 5.1 Atomic Transaction Boundary
Every transfer executes within a single PostgreSQL ACID transaction (`repository.WithTransaction`):
1. Insert `idempotency_records` in-progress record (arbitrates concurrent attempts).
2. Lock participating wallets in deterministic primary key order (`ORDER BY id ASC`).
3. Insert `transfers` record in `PENDING` status.
4. Execute conditional atomic debit on source wallet:
   ```sql
   UPDATE wallets
   SET balance = balance - $1, updated_at = NOW()
   WHERE id = $2 AND balance >= $1
   ```
5. If `RowsAffected == 0` (Insufficient Funds):
   - Transition transfer to `FAILED` (`failure_code = 'INSUFFICIENT_FUNDS'`).
   - Save 409 Conflict response in `idempotency_records`.
   - Commit transaction.
6. If `RowsAffected == 1` (Debit Success):
   - Credit destination wallet (`UPDATE wallets SET balance = balance + $1 WHERE id = $2`).
   - Insert 2 ledger records: 1 `DEBIT` (source) and 1 `CREDIT` (destination) of equal amounts.
   - Transition transfer to `PROCESSED`.
   - Save 201 Created response in `idempotency_records`.
   - Commit transaction.
7. Technical Error: Any system or unexpected failure rolls back the transaction completely, leaving no partial state or dirty records.

### 5.2 Deadlock Prevention
Opposing transfers (e.g. Wallet A $\rightarrow$ Wallet B and Wallet B $\rightarrow$ Wallet A) could deadlock if locks are acquired haphazardly. Sorting the wallet IDs lexicographically ensures both transactions acquire row locks in the exact same sequence (`ORDER BY id ASC`), eliminating deadlocks.

---

## 6. Idempotency Policy

1. **Same Key + Same Request (Previously Successful)**:
   Returns original saved HTTP `201 Created` response. No database balances or ledger entries are modified again.
2. **Same Key + Same Request (Previously Rejected for Insufficient Funds)**:
   Returns the saved HTTP `409 Conflict` failure response. The request is not re-evaluated even if the balance later increases; clients must submit a new request with a new idempotency key.
3. **Same Key + Different Request Parameters**:
   Returns HTTP `409 Conflict` with error code `IDEMPOTENCY_KEY_REUSE_MISMATCH`. No transfer is performed.
4. **Concurrent Requests with Same Key**:
   Arbitrated via PostgreSQL's unique constraint on `idempotency_key`. The first request performs the transfer; subsequent concurrent requests catch the unique constraint, wait for the winner to commit, and return the winner's committed response.
5. **Technical Failure**:
   Transaction rolls back; the key remains available for subsequent retries.

---

## 7. API Contract & Examples

### 7.1 Create Transfer

- **Endpoint**: `POST /transfers`
- **Headers**: `Content-Type: application/json`

#### Request:
```json
{
  "idempotencyKey": "transfer-demo-001",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 5000
}
```
*(Here, amount `5000` represents ₹50.00 in integer paise)*

#### Response — 201 Created (Success):
```json
{
  "id": "trn_d290f1ee-6c54-4b01-90e6-d701748f0851",
  "idempotencyKey": "transfer-demo-001",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 5000,
  "currency": "INR",
  "status": "PROCESSED",
  "createdAt": "2026-10-17T06:45:00Z"
}
```

#### Response — 409 Conflict (Insufficient Funds):
```json
{
  "error": {
    "code": "INSUFFICIENT_FUNDS",
    "message": "wallet wallet_1 has insufficient funds for transfer amount 5000000 paise"
  }
}
```

#### Response — 409 Conflict (Idempotency Key Reuse Mismatch):
```json
{
  "error": {
    "code": "IDEMPOTENCY_KEY_REUSE_MISMATCH",
    "message": "idempotency key was previously used with different transfer parameters"
  }
}
```

#### Response — 400 Bad Request (Validation Failure):
```json
{
  "error": {
    "code": "SAME_WALLET_TRANSFER",
    "message": "fromWalletId and toWalletId must be different"
  }
}
```

#### Response — 404 Not Found (Missing Wallet):
```json
{
  "error": {
    "code": "WALLET_NOT_FOUND",
    "message": "one or more specified wallets were not found"
  }
}
```

---

### 7.2 Get Wallet Balance

- **Endpoint**: `GET /wallets/{id}/balance`

#### Response — 200 OK:
```json
{
  "walletId": "wallet_1",
  "userId": "user_1",
  "balance": 995000,
  "currency": "INR",
  "updatedAt": "2026-10-17T06:45:00Z"
}
```

---

### 7.3 Get Transfer Details

- **Endpoint**: `GET /transfers/{id}`

#### Response — 200 OK:
```json
{
  "id": "trn_d290f1ee-6c54-4b01-90e6-d701748f0851",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 5000,
  "currency": "INR",
  "status": "PROCESSED",
  "createdAt": "2026-10-17T06:45:00Z",
  "updatedAt": "2026-10-17T06:45:00Z"
}
```

---

### 7.4 Health Check

- **Endpoint**: `GET /healthz`

#### Response — 200 OK:
```json
{
  "status": "healthy",
  "database": "connected"
}
```

---

## 8. Setup & Local Development

### Prerequisites
- [Go 1.22+](https://go.dev/) (Go 1.25 used)
- [Docker](https://www.docker.com/) & Docker Compose (optional for local PostgreSQL)

### 1. Configure Environment
Copy the example environment file:
```bash
cp .env.example .env
```

### 2. Start PostgreSQL via Docker Compose
```bash
docker compose up -d
```
This automatically starts PostgreSQL 16 on port `5432`, applies the schema migration `001_create_wallet_tables.sql`, and seeds initial test wallets:
- `wallet_1`: ₹10,000.00 (`1000000` paise)
- `wallet_2`: ₹5,000.00 (`500000` paise)

### 3. Run the Service
```bash
go run ./cmd/api
```
The API server starts listening on `http://localhost:8080`.

---

## 9. Running Migrations & Seed Data

### Versioned SQL Migration
To manually apply migrations against a PostgreSQL instance:
```bash
psql -h localhost -U postgres -d wallet_db -f migrations/001_create_wallet_tables.sql
```
Or with Docker Compose:
```bash
make migrate
```

### Seed Development Data
```bash
psql -h localhost -U postgres -d wallet_db -f scripts/seed.sql
```
Or with Docker Compose:
```bash
make seed
```

---

## 10. Running Tests

### Unit Tests
Run fast, isolated unit tests (testing domain validation, HTTP handler decoding, and service business rules with mock repository):
```bash
go test -v ./tests/unit/...
```

### PostgreSQL Integration Tests
Integration tests test real database transactions, atomic conditional updates, concurrent race conditions, duplicate idempotency keys, and rollbacks.

1. Set the dedicated test database URL:
```bash
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_test_db?sslmode=disable"
```
*(On Windows PowerShell: `$env:TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_test_db?sslmode=disable"`)*

2. Run integration tests:
```bash
go test -v -count=1 ./tests/integration/...
```

> **Note**: If `TEST_DATABASE_URL` is not set, integration tests skip safely with an informative message rather than silently claiming that integration behavior was verified.

### Run All Tests
```bash
go test -v ./...
```

### Format and Static Analysis
```bash
gofmt -s -w .
go vet ./...
```

---

## 11. Known Limitations & Trade-offs

1. **Single-Currency Support**:
   The service currently supports INR only. Multi-currency transfers would require dynamic exchange rate feeds, fx spread accounting, and currency-pair ledger records.
2. **Synchronous Transaction Boundaries**:
   Transfers commit synchronously as either `PROCESSED` or `FAILED`. For massive-scale cross-system banking transfers, an asynchronous saga or outbox pattern with two-phase commit might be necessary.
3. **Global Idempotency Keys**:
   Keys are globally unique strings. In large multi-tenant platforms, scoping idempotency keys per client/account (`(client_id, idempotency_key)`) may be desirable.
4. **Ledger Purging**:
   Ledger records are append-only audit records and should never be deleted. In high-volume production deployments, older ledger records should be partitioned by date (e.g. PostgreSQL declarative table partitioning by month).

---

## 12. Repository Structure

```text
├── cmd/api/main.go               # Entry point
├── internal/
│   ├── config/config.go          # Config loader
│   ├── domain/models.go          # Entities & domain models
│   ├── domain/errors.go          # Typed errors
│   ├── domain/money.go           # Money validation & formatting
│   ├── handler/                  # HTTP Handlers
│   ├── repository/               # GORM DB Repository
│   └── service/                  # Transfer Service
├── migrations/                   # SQL Migrations
├── scripts/                      # Seed Data
├── tests/
│   ├── unit/                     # Unit Tests
│   └── integration/              # PostgreSQL Integration Tests
├── docs/                         # Architecture & PR Documentation
├── docker-compose.yml            # Docker definition
└── Makefile                      # Build automation
```
