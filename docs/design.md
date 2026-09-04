# Wallet Transfer Service — Design Specification & Architecture

## 1. Overview & System Purpose

The **Wallet Transfer Service** is a high-reliability, transactionally consistent financial backend designed to handle wallet-to-wallet funds transfers. It provides **exactly-once execution semantics** at the API layer, guarantees **double-entry ledger balancing**, and prevents race conditions such as **double-spending** and **deadlocks** under high concurrency.

This document details the architectural design, database transaction model, idempotency guarantees, concurrency controls, error handling, state machine transitions, and testing strategy.

---

## 2. System Architecture & Layered Boundaries

The service follows a strict, clean four-layered architecture designed for domain isolation, maintainability, and testability.

```
                  +-----------------------------------+
                  |           HTTP Client             |
                  +-----------------------------------+
                                    |
                                    v
                  +-----------------------------------+
                  |     HTTP Handlers & Middleware    |
                  | (Gin, Request ID, Logging, CORS)  |
                  +-----------------------------------+
                                    |
                                    v
                  +-----------------------------------+
                  |           Service Layer           |
                  | (Orchestration, Idempotency Flow) |
                  +-----------------------------------+
                                    |
                                    v
                  +-----------------------------------+
                  |         Repository Layer          |
                  |   (PostgreSQL, Transactions,      |
                  |    pgxpool, Row Locking)          |
                  +-----------------------------------+
                                    |
                                    v
                  +-----------------------------------+
                  |        PostgreSQL Database        |
                  +-----------------------------------+
```

### Layer Responsibilities

1. **HTTP Handlers (`internal/handler`)**:
   - Parses HTTP payloads and path parameters.
   - Validates input syntax (UUID formats, positive monetary values, non-blank strings).
   - Maps domain/service errors into standard API JSON responses.
   - Contains **zero SQL queries** and **zero business rules**.

2. **Service Layer (`internal/service`)**:
   - Implements business logic and transfer workflows.
   - Coordinates idempotency record validation and SHA-256 payload hashing.
   - Orchestrates multi-step database transactions via the repository.

3. **Repository Layer (`internal/repository`)**:
   - Executes raw SQL queries via `pgxpool` with standard `context.Context` cancellation support.
   - Manages atomic PostgreSQL transaction boundaries (`pgx.Tx`).
   - Implements deterministic row-level locking (`SELECT ... FOR UPDATE ORDER BY id`).

4. **Domain Layer (`internal/domain`)**:
   - Defines pure domain entities (`Wallet`, `Transfer`, `LedgerEntry`, `IdempotencyRecord`).
   - Implements exact fixed-point monetary arithmetic (`Money`).
   - Encapsulates domain validation rules and immutable state machine transitions.

---

## 3. API Contract & Payload Definitions

### 3.1 Create Transfer (`POST /transfers`)

**Request**:
```json
{
  "idempotencyKey": "tx_req_9921_abc",
  "fromWalletId": "11111111-1111-1111-1111-111111111111",
  "toWalletId": "22222222-2222-2222-2222-222222222222",
  "amount": "100.50"
}
```

**Success Response (`201 Created` or `200 OK` for duplicate idempotency match)**:
```json
{
  "transferId": "33333333-3333-3333-3333-333333333333",
  "idempotencyKey": "tx_req_9921_abc",
  "fromWalletId": "11111111-1111-1111-1111-111111111111",
  "toWalletId": "22222222-2222-2222-2222-222222222222",
  "amount": "100.50",
  "status": "PROCESSED",
  "createdAt": "2026-09-04T15:30:00Z",
  "processedAt": "2026-09-04T15:30:00.045Z"
}
```

**Error Response Standard**:
```json
{
  "error": {
    "code": "INSUFFICIENT_FUNDS",
    "message": "source wallet has insufficient available balance for transfer"
  }
}
```

### 3.2 Get Wallet Balance (`GET /wallets/:id/balance`)
**Success Response (`200 OK`)**:
```json
{
  "walletId": "11111111-1111-1111-1111-111111111111",
  "balance": "899.50",
  "currency": "USD",
  "updatedAt": "2026-09-04T15:30:00Z"
}
```

### 3.3 Get Transfer (`GET /transfers/:id`)
**Success Response (`200 OK`)**:
```json
{
  "transferId": "33333333-3333-3333-3333-333333333333",
  "idempotencyKey": "tx_req_9921_abc",
  "fromWalletId": "11111111-1111-1111-1111-111111111111",
  "toWalletId": "22222222-2222-2222-2222-222222222222",
  "amount": "100.50",
  "status": "PROCESSED",
  "createdAt": "2026-09-04T15:30:00Z",
  "processedAt": "2026-09-04T15:30:00.045Z"
}
```

### 3.4 Get Wallet Ledger Transactions (`GET /wallets/:id/transactions`)
**Success Response (`200 OK`)**:
```json
{
  "walletId": "11111111-1111-1111-1111-111111111111",
  "entries": [
    {
      "entryId": "44444444-4444-4444-4444-444444444444",
      "transferId": "33333333-3333-3333-3333-333333333333",
      "type": "DEBIT",
      "amount": "100.50",
      "createdAt": "2026-09-04T15:30:00Z"
    }
  ]
}
```

---

## 4. Database Schema Design

The PostgreSQL database enforces constraints, referential integrity, and non-negative balances at the engine level.

```sql
-- 1. WALLETS TABLE
CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance NUMERIC(20, 4) NOT NULL DEFAULT 0.0000,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_wallets_balance_non_negative CHECK (balance >= 0.0000)
);

-- 2. TRANSFERS TABLE
CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NULL,
    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount NUMERIC(20, 4) NOT NULL,
    status VARCHAR(20) NOT NULL,
    error_code VARCHAR(50) NULL,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMPTZ NULL,
    CONSTRAINT chk_transfers_amount_positive CHECK (amount > 0.0000),
    CONSTRAINT chk_transfers_different_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT chk_transfers_status CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

-- 3. LEDGER_ENTRIES TABLE
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    type VARCHAR(10) NOT NULL,
    amount NUMERIC(20, 4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_ledger_amount_positive CHECK (amount > 0.0000),
    CONSTRAINT chk_ledger_type CHECK (type IN ('DEBIT', 'CREDIT'))
);

-- 4. IDEMPOTENCY_RECORDS TABLE
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    transfer_id UUID NULL REFERENCES transfers(id),
    response_code INT NOT NULL,
    response_body JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- INDEXES
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_transfers_idempotency_key ON transfers(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
```

---

## 5. Money & Precision Specification

1. **Floating-point prohibition**: `float32` and `float64` are strictly forbidden for monetary fields to avoid IEEE 754 precision artifacts.
2. **Database Representation**: `NUMERIC(20, 4)` allows up to 16 integer digits and 4 fractional digits (supporting sub-cent financial precision).
3. **Go Representation**: Exact decimal math via `github.com/shopspring/decimal`.
4. **Validation Rules**:
   - `amount > 0`
   - Maximum scale enforced (4 decimal places)
   - Rejection of invalid non-numeric strings or NaN/Inf inputs.

---

## 6. Concurrency & Deadlock Prevention Strategy

### 6.1 Race Conditions Addressed
When two concurrent requests attempt to debit the same source wallet (e.g. Wallet A = \$100; Request 1: A $\rightarrow$ B \$80; Request 2: A $\rightarrow$ C \$80), an optimistic read-then-write pattern (`SELECT balance`, `UPDATE balance`) leads to double-spending.

### 6.2 Solution: Deterministic Row-Level Locking
Inside the PostgreSQL transaction (`pgx.Tx`):
1. **Sort Wallet UUIDs**: Compare `fromWalletId` and `toWalletId` lexicographically.
   ```go
   firstID, secondID := fromID, toID
   if firstID > secondID {
       firstID, secondID = toID, fromID
   }
   ```
2. **Acquire Locks**: Execute `SELECT id, balance FROM wallets WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`.
3. **Lock Guarantee**: Because every transaction locks wallets in strict sorted UUID order, circular lock dependencies (Deadlocks) between two concurrent transfers involving the same wallet pair (e.g., A $\rightarrow$ B and B $\rightarrow$ A) are mathematically impossible.
4. **Balance Verification**: With exclusive locks acquired on both wallets, inspect `from_wallet.balance`. If `from_wallet.balance < amount`, abort and mark state as `FAILED` (or rollback).

---

## 7. Idempotency Strategy & Deduplication

### 7.1 Deduplication Workflow
When `POST /transfers` is called with an `idempotencyKey`:

```
                             +-------------------------------+
                             | Incoming Request + IdemKey    |
                             +-------------------------------+
                                             |
                                             v
                             +-------------------------------+
                             | Compute SHA-256 Request Hash  |
                             +-------------------------------+
                                             |
                                             v
                             +-------------------------------+
                             | Query idempotency_records     |
                             | inside Database Transaction   |
                             +-------------------------------+
                                             |
                       +---------------------+---------------------+
                       |                                           |
                 Record Exists                               Record Missing
                       |                                           |
        +--------------+--------------+             +--------------+--------------+
        |                             |             | Execute Atomic Transfer     |
  Hash Matches                  Hash Mismatch       | (Lock Wallets, Balance Check|
        |                             |             |  Debit/Credit, Ledger, State|
        v                             v             +--------------+--------------+
Return Cached Response     Return 409 CONFLICT                     |
(Original Status & Body)  "IDEMPOTENCY_KEY_REUSED"                 v
                                                    Save Record in idempotency_records
                                                    & Commit Transaction
```

### 7.2 Key Reuse Conflict
If `idempotencyKey` = `"key123"` was used for transferring \$100 from Wallet A to B, and later a request with `idempotencyKey` = `"key123"` arrives with amount \$200:
- The `request_hash` will mismatch.
- The system returns HTTP `409 Conflict` with error code `IDEMPOTENCY_KEY_REUSED`.
- No database mutations occur.

---

## 8. Transfer State Machine

```
              +-------------------+
              |      PENDING      |
              +-------------------+
                /               \
               /                 \
              v                   v
     +-----------------+  +-----------------+
     |    PROCESSED    |  |     FAILED      |
     +-----------------+  +-----------------+
```

### State Machine Constraints
- Valid transitions: `PENDING` $\rightarrow$ `PROCESSED`, `PENDING` $\rightarrow$ `FAILED`.
- Terminal states: `PROCESSED` and `FAILED` are terminal. No further transition allowed.
- Attempting invalid state mutations returns a domain error.

---

## 9. Failure Scenarios & Edge Cases

| Scenario | Handling Strategy | Result / Status |
| :--- | :--- | :--- |
| **Scenario 1**: Request succeeds, but client disconnects before receiving HTTP response. | Client retries request with same `idempotencyKey`. Service fetches cached record from `idempotency_records`. | Returns identical cached `201 Created` payload. |
| **Scenario 2**: Same `idempotencyKey` used with different payload. | Hashes mismatch in `idempotency_records`. | Returns `409 Conflict` (`IDEMPOTENCY_KEY_REUSED`). |
| **Scenario 3**: Two transfers attempt to spend same balance simultaneously. | Deterministic `SELECT ... FOR UPDATE`. First transaction deducts balance; second transaction sees balance insufficient. | 1st succeeds (`PROCESSED`), 2nd fails (`INSUFFICIENT_FUNDS`). |
| **Scenario 4**: Non-existent wallet ID provided. | Database foreign key check or lock query returns zero rows. | Returns `404 Not Found` (`WALLET_NOT_FOUND`). |
| **Scenario 5**: Source and Destination wallet IDs are identical. | Domain and Handler validation reject same wallet transfer. | Returns `400 Bad Request` (`SAME_WALLET_TRANSFER`). |
| **Scenario 6**: Database connection breaks mid-transaction. | PostgreSQL auto-rolls back uncommitted transaction. Ledger and balances remain untouched. | Returns `500 Internal Server Error`. Client can safely retry. |

---

## 10. Observability Strategy

- **Structured Logging (`log/slog`)**: JSON formatted logs including fields: `time`, `level`, `request_id`, `method`, `path`, `status`, `duration_ms`, `error_code`.
- **Request ID Middleware**: Injects or propagates `X-Request-ID` header into Gin context and log fields.
- **Health Checks**:
  - `GET /health/live`: Returns `200 OK` (liveness check for process).
  - `GET /health/ready`: Checks PostgreSQL `PingContext()`; returns `200 OK` or `503 Service Unavailable`.

---

## 11. Testing Strategy

1. **Unit Tests**:
   - Fixed-point monetary arithmetic and formatting.
   - Domain entity state transition validations.
   - HTTP Handler parsing and status code mapping.
2. **Integration Tests**:
   - Real PostgreSQL execution using test containers/database.
   - Successful transfer atomic verification (balance updates + 2 ledger entries).
   - Insufficient balance rollback.
   - Idempotent duplicate replay & key reuse conflict detection.
3. **Concurrency Tests**:
   - Launching parallel goroutines attempting concurrent debits on a single wallet.
   - Verifying zero negative balances, zero double-spending, and exact ledger balancing.
