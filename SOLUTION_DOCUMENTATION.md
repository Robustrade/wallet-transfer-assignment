# Wallet Transfer Service Documentation

A production-grade, highly reliable, and concurrency-safe **Wallet Transfer Service** built in Go and PostgreSQL, demonstrating double-entry ledger accounting, exactly-once request processing via idempotency keys, deterministic deadlock prevention, and clean layered architecture.

---

## Key Features

- **Exactly-Once Semantics (Idempotency)**: Requests with an `idempotencyKey` execute atomically within a database transaction. Replaying identical requests returns original cached response payloads without duplicating transfers or ledger entries.
- **Double-Entry Ledger Accounting**: Every transfer creates exactly two ledger entries (1 `DEBIT` and 1 `CREDIT`). Ledger balances are 100% verifiable against stored wallet balances via audit endpoints.
- **Deterministic Lock Ordering**: Row locking uses `ORDER BY id ASC` during `SELECT ... FOR UPDATE` to guarantee deadlock-free execution under high-concurrency reciprocal transfers ($W_1 \rightarrow W_2$ and $W_2 \rightarrow W_1$).
- **Precision Monetary Accounting**: Amounts and balances are stored as integer minor units (e.g. cents as `int64`), eliminating floating-point rounding bugs.
- **PostgreSQL Persistence Engine**: Uses `github.com/jackc/pgx/v5` for ACID database transactions, table constraints, and foreign key integrity.

---

## Architecture Overview

```
[ HTTP Client / API ]
         │
         ▼
 ┌────────────────┐
 │ Handler Layer  │  (HTTP request parsing, status code mapping, JSON serialization)
 └───────┬────────┘
         │
         ▼
 ┌────────────────┐
 │ Service Layer  │  (Business rules, idempotency orchestration, double-entry ledger)
 └───────┬────────┘
         │
         ▼
 ┌────────────────┐
 │ Repository     │  (Transactional SQL queries, deterministic SELECT FOR UPDATE locking)
 └───────┬────────┘
         │
         ▼
 ┌────────────────┐
 │ PostgreSQL DB  │  (ACID database transactions & relational constraints)
 └────────────────┘
```

---

## Database Schema Design

### `wallets`
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `VARCHAR(64)` | `PRIMARY KEY` | Wallet account identifier |
| `balance` | `BIGINT` | `NOT NULL CHECK (balance >= 0)` | Stored balance in minor units (cents) |
| `currency` | `VARCHAR(3)` | `NOT NULL DEFAULT 'USD'` | Currency code |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL` | Creation timestamp |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL` | Last update timestamp |

### `transfers`
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `VARCHAR(64)` | `PRIMARY KEY` | Transfer transaction identifier |
| `idempotency_key` | `VARCHAR(255)` | `UNIQUE NOT NULL` | Idempotency deduplication key |
| `from_wallet_id` | `VARCHAR(64)` | `FK -> wallets.id` | Source wallet ID |
| `to_wallet_id` | `VARCHAR(64)` | `FK -> wallets.id` | Destination wallet ID |
| `amount` | `BIGINT` | `NOT NULL CHECK (amount > 0)` | Transfer amount in minor units |
| `status` | `VARCHAR(32)` | `NOT NULL` | State: `PENDING`, `PROCESSED`, `FAILED` |
| `failure_reason` | `TEXT` | `NULL` | Error details for failed transfers |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL` | Timestamp |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL` | Timestamp |

### `ledger_entries`
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `VARCHAR(64)` | `PRIMARY KEY` | Ledger entry identifier |
| `transfer_id` | `VARCHAR(64)` | `FK -> transfers.id` | Associated transfer ID |
| `wallet_id` | `VARCHAR(64)` | `FK -> wallets.id` | Target wallet ID |
| `entry_type` | `VARCHAR(16)` | `NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT'))` | Accounting entry type |
| `amount` | `BIGINT` | `NOT NULL CHECK (amount > 0)` | Entry amount in minor units |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL` | Creation timestamp |

*Uniqueness Constraint*: `CONSTRAINT uniq_ledger_transfer_type UNIQUE (transfer_id, entry_type)`

### `idempotency_records`
| Column | Type | Constraints | Description |
|---|---|---|---|
| `idempotency_key` | `VARCHAR(255)` | `PRIMARY KEY` | Client idempotency key |
| `request_hash` | `VARCHAR(64)` | `NOT NULL` | SHA-256 hash of payload inputs |
| `status` | `VARCHAR(32)` | `NOT NULL` | `IN_PROGRESS`, `COMPLETED`, `FAILED` |
| `response_code` | `INT` | `NULL` | HTTP status code for replay |
| `response_body` | `TEXT` | `NULL` | JSON response payload |
| `transfer_id` | `VARCHAR(64)` | `NULL` | Transfer ID if created |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL` | Timestamp |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL` | Timestamp |

---

## API Endpoints

### 1. Create Wallet Transfer
`POST /transfers`

**Request Body**:
```json
{
  "idempotencyKey": "tx_idem_99812",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 1000
}
```

**Response** (`201 Created` or `200 OK` on replay):
```json
{
  "id": "trn_8f9a2b1c3d4e5f6a",
  "idempotencyKey": "tx_idem_99812",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 1000,
  "status": "PROCESSED",
  "createdAt": "2026-08-09T16:45:00Z",
  "updatedAt": "2026-08-09T16:45:00Z"
}
```

### 2. Get Transfer Details
`GET /transfers/{id}`

### 3. Create Wallet
`POST /wallets`

**Request Body**:
```json
{
  "id": "wallet_1",
  "initialBalance": 5000,
  "currency": "USD"
}
```

### 4. Get Wallet Details
`GET /wallets/{id}`

### 5. Audit Wallet Ledger Balance
`GET /wallets/{id}/audit`

---

## How to Run & Test

### Prerequisites
- Go 1.24+
- Docker & Docker Compose (for PostgreSQL)

### Running PostgreSQL locally
```bash
docker run -d --name wallet-postgres -p 5432:5432 -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=wallet_db postgres:16-alpine
```

### Running the HTTP Server
```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable"
export PORT="8080"
go run ./cmd/server
```

### Running Test Suite
```bash
go test -v -race -cover -p 1 ./...
```

---

## AI Usage Disclosure

Per `ASSIGNMENT.md` guidelines, full AI disclosure is provided:

1. **Tool Used**: Antigravity AI Coding Assistant powered by Claude 3.7 Sonnet / Gemini models.
2. **Usage Pattern**: Used for system architecture design, database lock ordering analysis, double-entry accounting schema design, table-driven unit test generation, and high-concurrency race condition testing.
3. **Session Logs**: Conversation transcripts and decision logs are persisted under `<appDataDir>/brain/47cd1d3c-1daf-43af-952d-13b0706d8c3b/`.
