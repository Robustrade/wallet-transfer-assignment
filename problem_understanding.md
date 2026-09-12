# Wallet Transfer Service — Problem Understanding & Technical Approach

## 1. Executive Summary

The objective of this assignment is to design and implement a production-grade, highly reliable, and transactional **Wallet Transfer Service** in Go. 

Financial and payment systems operate in distributed and concurrent environments where partial failures, network retries, and simultaneous transactions are the norm rather than the exception. A simple "read balance, subtract, write balance" approach fails catastrophically under concurrent access (double spending) or network retries (duplicate debits). 

This document defines the comprehensive problem analysis, domain models, database schema, concurrency controls, idempotency guarantees, layered software architecture, and testing strategy prior to code implementation.

---

## 2. Core Requirements & Challenges

### 2.1 Functional Requirements

1. **Transfer Execution (`POST /transfers`)**:
   - Transfer funds atomically between a source wallet (`fromWalletId`) and a destination wallet (`toWalletId`).
   - Validate input parameters: positive non-zero amount, distinct source and destination wallets, existence of both wallets.
   - Reject transfers if the source wallet has insufficient funds.

2. **Idempotent Request Handling**:
   - Client supplies an `idempotencyKey` in the request.
   - Re-submitting a request with the same `idempotencyKey` must return the original result without re-executing the transfer or duplicating side effects.
   - Supplying the same `idempotencyKey` with different transfer parameters must be detected as a conflict (`409 Conflict`).

3. **Double-Entry Ledger Recording**:
   - Every executed transfer must create exactly **two** immutable ledger entries within the same database transaction:
     - `DEBIT` entry for the source wallet.
     - `CREDIT` entry for the destination wallet.
   - The ledger must strictly balance: for every transfer, $\sum \text{Debit} = \sum \text{Credit}$.

4. **Wallet Balance Consistency**:
   - Balances must remain strictly correct and non-negative ($balance \ge 0$) under high concurrency.
   - Strategy: Stored balance in `wallets` table updated within the transaction for $O(1)$ balance queries and fast validation, backed by immutable `ledger_entries` for auditing and ledger reconciliation.

5. **Transfer State Machine**:
   - Allowed states: `PENDING`, `PROCESSED`, `FAILED`.
   - Transitions:
     - `PENDING -> PROCESSED` (on successful debit, credit, ledger write, and commit).
     - `PENDING -> FAILED` (on business validation failure such as insufficient funds).
   - State transitions must be guarded and deterministic.

---

## 3. Deep-Dive Design Strategies

### 3.1 Concurrency Control & Deadlock Prevention

#### The Race Condition (Double Spend)
If Wallet A has a balance of $100 and two concurrent requests attempt to transfer $80:
- Without concurrency control, both transactions read $balance = 100$, verify $100 \ge 80$, subtract $80$, and write $balance = 20$.
- Result: $160 transferred from a $100 account (double spend, invalid negative balance).

#### The Deadlock Hazard (Circular Wait)
If User 1 transfers from Wallet A to Wallet B while User 2 transfers from Wallet B to Wallet A concurrently:
- Transaction 1 locks Wallet A, then requests a lock on Wallet B.
- Transaction 2 locks Wallet B, then requests a lock on Wallet A.
- Result: **Deadlock** (circular dependency). Database aborts one or both transactions.

#### The Solution: Deterministic Row Lock Ordering
To eliminate both race conditions and deadlocks:
1. **Pessimistic Row-Level Locking**: Acquire row locks on both wallets using `SELECT ... FOR UPDATE` within a `READ COMMITTED` PostgreSQL transaction. This guarantees that modifications to the same wallets are serialized, preventing lost updates and race conditions without relying on optimistic retry loops.
2. **Deterministic Lock Ordering**: Always acquire locks on `min(fromWalletId, toWalletId)` first, followed by `max(fromWalletId, toWalletId)`.
   - Even if transfers occur in opposite directions ($A \to B$ and $B \to A$), both transactions lock in the identical order ($\min(A, B)$ then $\max(A, B)$).
   - By eliminating circular wait conditions (Coffman conditions), deadlocks become mathematically impossible.
3. **Database Check Constraint**: Add `CHECK (balance >= 0)` on the `wallets` table as a defense-in-depth guarantee against software bugs.

```
       Transfer 1 (A -> B)                 Transfer 2 (B -> A)
               │                                   │
               ▼                                   ▼
       Order: min(A, B) -> A               Order: min(A, B) -> A
               │                                   │
       Acquires Lock on A                  Waits for Lock on A
               │                                   │
       Acquires Lock on B                          ┆
               │                                   ┆
        Executes & Commits                         ┆
               │                                   ▼
        Releases Locks ───────────────>    Acquires Lock on A
                                                   │
                                           Acquires Lock on B
                                                   │
                                            Executes & Commits
```

---

### 3.2 Idempotency & Exactly-Once Semantics

To guarantee safe retries across network drops and client retransmissions:

1. **Idempotency Record Storage**:
   - An `idempotency_records` table tracks every request by `idempotency_key`.
   - Columns include:
     - `idempotency_key` (VARCHAR, PRIMARY KEY)
     - `request_hash` (SHA-256 digest of the canonical length-delimited representation: `len(from) + ":" + from + ":" + len(to) + ":" + to + ":" + amount`, eliminating delimiter-injection collisions such as `("a", "b:c")` vs `("a:b", "c")`)
     - `transfer_id` (UUID/TEXT, NULLABLE)
     - `status` (`IN_PROGRESS`, `COMPLETED`, `FAILED`)
     - `response_code` (INT)
     - `response_body` (TEXT)
     - `created_at`, `updated_at`

2. **Handling Scenarios & Short Committed Reservation Protocol**:
   - **Why Reservations Cannot Be Inside the Transfer Transaction**:
     - In PostgreSQL, if an `IN_PROGRESS` reservation is inserted inside the same transaction as the transfer, concurrent duplicate requests executing `INSERT ... ON CONFLICT` block on the unique-index tuple until the first transaction commits or rolls back.
     - When the first transaction commits, concurrent requests wake up and observe `COMPLETED`, completely bypassing the documented `IN_PROGRESS` / `409 Conflict` behavior and holding connection pool connections for the full transfer duration.
   - **The Short Committed Reservation Architecture**:
     1. **Key Validation & Resolution Precedence**:
        - Validate `idempotency_key` is not empty (returns `400 Bad Request` if missing).
        - Compute canonical `request_hash`.
        - Resolve the existing key against the fingerprint **before** running business-field validation. Reusing an existing key with altered parameters (even if invalid, e.g. `amount <= 0` or missing wallet) returns `409 Conflict` (`ErrIdempotencyConflict`), ensuring that idempotency conflict detection takes precedence over body validation.
     2. **Short Committed Reservation**:
        - Execute a standalone atomic `INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING` committed immediately *outside* the transfer transaction.
        - Overlapping concurrent requests immediately observe the committed `IN_PROGRESS` status and return `409 Conflict` (`ErrIdempotencyInProgress`) without waiting or exhausting pool connections.
      3. **Atomic Stale-Owner Recovery & Background Heartbeat**:
         - While a transfer executes, a background heartbeat periodically refreshes `updated_at` (every 5 seconds) to prevent active long-running transactions from being reclaimed.
         - If an `IN_PROGRESS` record's `updated_at` is older than `stale_timeout` (e.g. 30 seconds, indicating a crashed node or abandoned request), a new request atomically reclaims ownership via compare-and-swap with a unique `owner_token`:
           ```sql
           UPDATE idempotency_records
           SET request_hash = $1, owner_token = $2, status = 'IN_PROGRESS', updated_at = $3
           WHERE idempotency_key = $4 AND status = 'IN_PROGRESS' AND updated_at <= $5
           RETURNING idempotency_key;
           ```
      4. **Deferred Cleanup on Unexpected Abort**:
         - If execution aborts unexpectedly (e.g. context cancellation, network disconnection) before reaching a terminal state (`COMPLETED` or `FAILED`), a deferred cleanup hook calls:
           ```sql
           DELETE FROM idempotency_records 
           WHERE idempotency_key = $1 AND owner_token = $2 AND status = 'IN_PROGRESS';
           ```
           Including `owner_token = $2` prevents an old worker that was reclaimed after a timeout from deleting a newer worker's active reservation.
      5. **Atomic Finalization**:
         - The transfer transaction updates the idempotency record to `COMPLETED` (HTTP 200/201) or `FAILED` (HTTP 422 for insufficient funds) atomically with wallet balances and ledger entries.
    - **Duplicate Arrival (Existing Key, Identical Payload)**:
      - If record status is `COMPLETED` or `FAILED`: immediately return the cached `response_code` and `response_body` with `isReplay: true`. Zero side effects triggered.
      - If record status is `IN_PROGRESS`: another request with the same key is currently running. Return `409 Conflict` (`ErrIdempotencyInProgress`) immediately.
    - **Key Collision (Existing Key, Different Payload)**:
      - If `request_hash` does not match the stored hash: return `409 Conflict` (`ErrIdempotencyConflict`) with message: `"idempotency key reused with different request payload"`.

---

### 3.3 Double-Entry Ledger Principles

The system adheres to standard financial accounting principles:
1. **Immutability**: Ledger entries are append-only. They are never updated or deleted.
2. **Balanced Transaction**: Every transfer produces exactly two records:
   - Debit: Source Wallet ID, Transfer ID, Type `DEBIT`, Amount.
   - Credit: Destination Wallet ID, Transfer ID, Type `CREDIT`, Amount.
3. **Auditability**:
   $$\text{Wallet Balance} = \sum \text{Credits} - \sum \text{Debits}$$
   A ledger verification utility ensures that the stored wallet balance strictly matches the calculated balance derived from all historic ledger entries.

---

## 4. Proposed Database Schema

The database design uses **PostgreSQL** as the primary relational database, leveraging native row-level locks (`SELECT ... FOR UPDATE`), transaction isolation, and ACID guarantees.

A `docker-compose.yml` configuration is provided with a persistent volume (`pgdata`), ensuring all database files are permanently stored on host storage across container stops, restarts, and machine reboots.

```sql
-- Wallets table
CREATE TABLE wallets (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Transfers table
CREATE TABLE transfers (
    id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(128) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status VARCHAR(32) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT check_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

-- Double-entry ledger entries table
CREATE TABLE ledger_entries (
    id VARCHAR(64) PRIMARY KEY,
    transfer_id VARCHAR(64) REFERENCES transfers(id) ON DELETE RESTRICT, -- Nullable for genesis/opening balance entries without an associated transfer
    wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    type VARCHAR(16) NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT check_ledger_transfer_id CHECK (transfer_id IS NOT NULL OR (id = 'entry_system_treasury_opening' AND wallet_id = 'system_treasury' AND type = 'CREDIT'))
);

-- Idempotency tracking table
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(128) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    owner_token VARCHAR(64) NOT NULL DEFAULT '',
    transfer_id VARCHAR(64) REFERENCES transfers(id),
    status VARCHAR(32) NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    response_code INT,
    response_body TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance and integrity
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_ledger_wallet ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE UNIQUE INDEX idx_ledger_entries_transfer_type ON ledger_entries (transfer_id, type) WHERE transfer_id IS NOT NULL;
CREATE UNIQUE INDEX idx_ledger_entries_transfer_wallet ON ledger_entries (transfer_id, wallet_id) WHERE transfer_id IS NOT NULL;

-- Database-level constraint trigger enforcing that every transfer has exactly two balanced entries:
-- exactly one DEBIT on from_wallet_id and one CREDIT on to_wallet_id, matching transfer amount.
CREATE OR REPLACE FUNCTION check_ledger_pair_integrity()
RETURNS TRIGGER AS $$
DECLARE
    transfer_rec RECORD;
    debit_count INT;
    credit_count INT;
    debit_sum BIGINT;
    credit_sum BIGINT;
BEGIN
    IF NEW.transfer_id IS NOT NULL THEN
        SELECT * INTO transfer_rec FROM transfers WHERE id = NEW.transfer_id;
        IF FOUND THEN
            IF transfer_rec.status <> 'PROCESSED' THEN
                RAISE EXCEPTION 'invalid ledger pair for transfer %: transfer status must be PROCESSED at commit time, got %', NEW.transfer_id, transfer_rec.status;
            END IF;

            SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO debit_count, debit_sum
            FROM ledger_entries
            WHERE transfer_id = NEW.transfer_id AND type = 'DEBIT' AND wallet_id = transfer_rec.from_wallet_id;

            SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO credit_count, credit_sum
            FROM ledger_entries
            WHERE transfer_id = NEW.transfer_id AND type = 'CREDIT' AND wallet_id = transfer_rec.to_wallet_id;

            IF debit_count <> 1 OR credit_count <> 1 OR debit_sum <> transfer_rec.amount OR credit_sum <> transfer_rec.amount THEN
                RAISE EXCEPTION 'invalid ledger pair for transfer %: must have exactly 1 DEBIT on from_wallet and 1 CREDIT on to_wallet matching transfer amount', NEW.transfer_id;
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_check_ledger_pair
AFTER INSERT OR UPDATE ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_ledger_pair_integrity();

-- Database-level constraint trigger on transfers enforcing that no transfer can be committed as PROCESSED
-- without an exact, balanced double-entry pair existing in ledger_entries, and rejecting invalid mutations.
CREATE OR REPLACE FUNCTION check_transfer_processed_ledger_pair()
RETURNS TRIGGER AS $$
DECLARE
    current_status VARCHAR(20);
    debit_count INT;
    credit_count INT;
    debit_sum BIGINT;
    credit_sum BIGINT;
BEGIN
    -- Reject invalid status mutations to terminal transfers at the database boundary
    IF TG_OP = 'UPDATE' AND OLD.status IN ('PROCESSED', 'FAILED') AND NEW.status != OLD.status THEN
        RAISE EXCEPTION 'cannot update terminal transfer % (status: %) to %', OLD.id, OLD.status, NEW.status;
    END IF;

    -- Look up the actual commit-time status of the transfer
    SELECT status INTO current_status FROM transfers WHERE id = NEW.id;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF current_status = 'PROCESSED' THEN
        SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO debit_count, debit_sum
        FROM ledger_entries
        WHERE transfer_id = NEW.id AND type = 'DEBIT' AND wallet_id = NEW.from_wallet_id;

        SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO credit_count, credit_sum
        FROM ledger_entries
        WHERE transfer_id = NEW.id AND type = 'CREDIT' AND wallet_id = NEW.to_wallet_id;

        IF debit_count <> 1 OR credit_count <> 1 OR debit_sum <> NEW.amount OR credit_sum <> NEW.amount THEN
            RAISE EXCEPTION 'invalid ledger pair for transfer %: cannot commit PROCESSED transfer: requires exactly 1 DEBIT on from_wallet (%) and 1 CREDIT on to_wallet (%) matching transfer amount (%)',
                NEW.id, NEW.from_wallet_id, NEW.to_wallet_id, NEW.amount;
        END IF;
    ELSIF current_status = 'FAILED' THEN
        SELECT COUNT(*) INTO debit_count
        FROM ledger_entries
        WHERE transfer_id = NEW.id;

        IF debit_count > 0 THEN
            RAISE EXCEPTION 'cannot commit FAILED transfer % with existing ledger entries', NEW.id;
        END IF;
    ELSE
        -- Non-terminal status (such as PENDING) must not have ledger entries attached at commit
        SELECT COUNT(*) INTO debit_count
        FROM ledger_entries
        WHERE transfer_id = NEW.id;

        IF debit_count > 0 THEN
            RAISE EXCEPTION 'cannot commit non-terminal transfer % (status: %) with existing ledger entries', NEW.id, current_status;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_check_transfer_processed
AFTER INSERT OR UPDATE ON transfers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_transfer_processed_ledger_pair();

-- Immutability trigger on ledger_entries: prevent updates and deletes to protect the audit trail
CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are immutable: deletions and updates are forbidden';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_prevent_ledger_mutation
BEFORE DELETE OR UPDATE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_mutation();
```

> **Note on Double-Entry Ledger Enforcement Boundaries**:
> Ledger integrity is protected at complementary boundaries:
> 1. **Application & Transaction Boundary**: The service and repository layers strictly create ledger entries in balanced atomic pairs via `domain.NewDoubleEntryPair(transferID, fromID, toID, amount)` and `repos.Ledger.CreateLedgerEntries(ctx, debit, credit)` within the transfer transaction. Multi-row atomic SQL inserts guarantee statement-level atomicity even when invoked against connection pools.
> 2. **Database Constraint Trigger Boundary**: PostgreSQL constraint triggers validate ledger integrity at transaction commit:
>    - `trg_check_ledger_pair` (on `ledger_entries`, deferred): Validates that any transfer referenced in `ledger_entries` has exactly one DEBIT on `from_wallet_id` and one CREDIT on `to_wallet_id` with matching sums.
>    - `trg_check_transfer_processed` (on `transfers`, deferred): Validates that no transfer can commit with status `PROCESSED` without exactly one DEBIT on `from_wallet_id` and one CREDIT on `to_wallet_id` matching `amount`.
>    - `trg_prevent_ledger_mutation` (on `ledger_entries`): Guarantees absolute append-only immutability by forbidding any `UPDATE` or `DELETE` on ledger entries.

> **Note on Money Representation**: All monetary values are represented as `BIGINT` representing minor currency units (e.g. cents) to avoid IEEE 754 floating-point rounding errors.
>
> **Note on Schema Naming & Consistency**: Table names (`wallets`, `transfers`, `ledger_entries`, `idempotency_records`) and foreign keys strictly align across DDL, domain models, and repositories. The primary key of `idempotency_records` is explicitly `idempotency_key` (avoiding reserved SQL keywords). In `ledger_entries`, `id` represents the unique entry identifier (`entry_id`).
>
> **Note on `updated_at` Semantics**: In PostgreSQL, `DEFAULT CURRENT_TIMESTAMP` sets the initial timestamp on `INSERT`, but PostgreSQL does not automatically update this column upon `UPDATE` statements without a trigger. To maintain explicit, auditable control and avoid database trigger side effects, the application layer explicitly updates `updated_at = time.Now().UTC()` in all `UPDATE` queries across repositories. Note that `ledger_entries` is append-only/immutable by accounting design, and therefore intentionally contains only `created_at` with no `updated_at`.
---

## 5. Clean Layered Architecture

The project will follow clean architecture principles, separating concerns cleanly across 4 layers:

```
┌─────────────────────────────────────────────────────────┐
│                      HTTP Handlers                      │
│   (Request decoding, JSON validation, HTTP responses)   │
└────────────────────────────┬────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────┐
│                     Service Layer                       │
│    (Business orchestration, idempotency logic, locks,   │
│       state transitions, balance checks, transactions)   │
└────────────────────────────┬────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────┐
│                    Repository Layer                     │
│  (Database access, SQL queries, transaction management) │
└────────────────────────────┬────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────┐
│                      Domain Models                      │
│     (Core entities, Value Objects, Domain Errors)       │
└─────────────────────────────────────────────────────────┘
```

### Directory Structure
```text
wallet-transfer-assignment/
├── cmd/
│   └── api/
│       └── main.go                 # Application bootstrap & dependency injection
├── internal/
│   ├── domain/                     # Pure domain models and domain errors
│   │   ├── wallet.go
│   │   ├── transfer.go
│   │   ├── ledger.go
│   │   ├── idempotency.go
│   │   └── errors.go
│   ├── repository/                 # Data access interfaces and implementations
│   │   ├── repository.go           # Repository interfaces & TxManager
│   │   └── postgres/               # PostgreSQL implementation
│   │       ├── db.go
│   │       ├── wallet_repo.go
│   │       ├── transfer_repo.go
│   │       ├── ledger_repo.go
│   │       └── idempotency_repo.go
│   ├── service/                    # Application service & business logic
│   │   ├── transfer_service.go
│   │   ├── wallet_service.go
│   │   └── transfer_service_test.go
│   └── handler/                    # HTTP REST transport
│       ├── handler.go
│       ├── transfer_handler.go
│       ├── wallet_handler.go
│       └── router.go
├── pkg/
│   └── testutil/                   # Test fixtures, database helpers
├── go.mod
├── go.sum
└── problem_understanding.md
```

---

## 6. API Specifications

### 6.1 Create Transfer
- **Endpoint**: `POST /transfers`
- **Headers**: `Content-Type: application/json`
- **Request Body**:
```json
{
  "idempotencyKey": "tx-uuid-12345",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

- **Successful Response (`201 Created` or `200 OK`)**:
```json
{
  "transferId": "tr-abc-987",
  "idempotencyKey": "tx-uuid-12345",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "createdAt": "2026-09-10T17:30:00Z"
}
```

- **Error Responses**:
  - `400 Bad Request`: Invalid payload, missing fields, non-positive amount, identical from/to wallet.
  - `404 Not Found`: Source or destination wallet does not exist.
  - `409 Conflict`: Idempotency key currently in-flight, or idempotency key reused with mismatched request parameters.
  - `422 Unprocessable Entity`: Insufficient funds in source wallet.
  - `500 Internal Server Error`: Unhandled database or operational error.

### 6.2 Supplementary Endpoints (For Inspection & Testing)
- `POST /wallets`: Create unseeded wallet (initialBalance must be 0 for public callers; positive initial balances require administrative authorization).
- `POST /admin/wallets`: Create wallet with initial balance funded from system treasury (requires administrative authorization via `X-Admin-Key` or Bearer token).
- `GET /wallets/{id}`: Retrieve wallet info and current balance.
- `GET /transfers/{id}`: Retrieve transfer details.
- `GET /wallets/{id}/ledger`: Retrieve bounded paginated ledger history (`?limit=50&offset=0`) for audit verification.
- `GET /wallets/{id}/reconcile`: Audit stored wallet balance against historical ledger entries.

---

## 7. Testing Discipline & Verification Strategy

Following the Red-Green-Refactor (TDD) development discipline and comprehensive testing requirements:
1. **Unit Testing**:
   - Domain entity validation (transfer rules, amount validations, state machine transitions).
   - Mock-based service testing for error branches and boundary conditions.

2. **Integration Testing**:
   - Real database operations testing repositories and transactional boundaries.
   - Idempotency replay testing:
     - Replaying exact same request -> identical response, no duplicate debit.
     - In-flight concurrent duplicate request -> rejected / 409 Conflict.
     - Payload mismatch with same key -> 409 Conflict.
   - Ledger integrity testing:
     - Verify two ledger rows exist for every transfer.
     - Verify sum of debits == sum of credits.
     - Verify wallet balance == sum of historical ledger entries.

3. **Concurrency & Race Condition Testing**:
   - **Double-Spend Stress Test**:
     - Wallet seeded with $100.
     - 10 concurrent goroutines attempt to transfer $20 each.
     - Exactly 5 must succeed and 5 must fail with insufficient funds. Final balance must be exactly $0.
   - **Deadlock Stress Test**:
     - 40 concurrent bidirectional transfers (20 from Wallet A to Wallet B and 20 from Wallet B to Wallet A simultaneously).
     - All must complete without deadlock errors, and net balances must match arithmetic expectations.

---

## 8. Summary of Engineering Tradeoffs

| Decision | Chosen Approach | Rationale |
| :--- | :--- | :--- |
| **Balance Strategy** | Stored balance + Immutable Double-Entry Ledger | $O(1)$ fast balance checks for transfers while retaining full non-repudiation and auditability through double-entry ledger. |
| **Concurrency Control** | Deterministic row locking (`min/max` ID order) | Eliminates race conditions and prevents deadlocks mathematically without resorting to coarse global mutexes. |
| **Money Representation**| Integer minor units (`BIGINT`) | Completely eliminates IEEE-754 floating point inaccuracies. |
| **Idempotency Strategy**| Dedicated idempotency table + payload hash | Durable across restarts, detects payload tampering/collisions, and preserves original response body. |
| **Database Engine** | PostgreSQL (with `jackc/pgx/v5`) | Industry-standard transactional engine supporting true row-level locks (`SELECT FOR UPDATE`), strict constraints, and connection pooling. Docker Compose provided for 1-command startup. |

---

## 9. How to Run

### 9.1 Prerequisites
- **Go**: 1.23.1 or newer (matching `go.mod`)
- **Docker & Docker Compose**: For local PostgreSQL cluster
- **PostgreSQL Client (Optional)**: `psql` for database inspection

### 9.2 Start Database Services
Start the PostgreSQL container with persistent storage and initialized roles/databases:
```bash
docker-compose up -d
```
The compose file spins up PostgreSQL on port `5432` with credentials:
- Superuser: `postgres`
- Superuser Password: `postgrespassword`
- Primary Database: `wallet_db`
- Test Database: `wallet_test_db` (provisioned via `scripts/init-user.sql`)
- Least-Privilege Application User: `wallet_app` (Password: `wallet_app_password`)

### 9.3 Configuration Environment Variables
The application reads configuration from environment variables with production-safe defaults:
| Variable | Description | Default | Required in Non-Dev |
| :--- | :--- | :--- | :--- |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://wallet_app:wallet_app_password@localhost:5432/wallet_db?sslmode=disable` (dev only) | Yes |
| `PORT` | HTTP server listening port | `8080` | No |
| `APP_ENV` | Application environment (`development`, `production`, etc.) | `production` (safe default) | No |
| `ADMIN_KEY` | Bearer token for `/admin/*` endpoints | `admin-secret-dev` (only if `APP_ENV=development` & default URL) | Yes |

### 9.4 Start the Application
To run the server in development mode:
```bash
# Set development environment and run
export APP_ENV=development
go run ./cmd/api
```
*(On Windows PowerShell:)*
```powershell
$env:APP_ENV = "development"
go run ./cmd/api
```

Database migrations in `migrations/` are automatically applied on server startup.

### 9.5 Sample API Commands

#### 1. Create a Treasury-Funded Wallet (Admin)
```bash
curl -X POST http://localhost:8080/admin/wallets \
  -H "Authorization: Bearer admin-secret-dev" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "alice_wallet",
    "name": "Alice",
    "initialBalance": 1000
  }'
```

#### 2. Create a Standard Wallet (Public)
```bash
curl -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{
    "id": "bob_wallet",
    "name": "Bob",
    "initialBalance": 0
  }'
```

#### 3. Execute an Idempotent Transfer
```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "txn-001-uuid",
    "fromWalletId": "alice_wallet",
    "toWalletId": "bob_wallet",
    "amount": 250
  }'
```

#### 4. Query Wallet Balance
```bash
curl http://localhost:8080/wallets/alice_wallet
```

#### 5. Inspect Double-Entry Ledger with Pagination
```bash
curl "http://localhost:8080/wallets/alice_wallet/ledger?limit=10&offset=0"
```

---

## 10. How to Test

### 10.1 Running the Full Test Suite
The test suite executes against an isolated PostgreSQL test database (`wallet_test_db`). To run all tests across all packages:
```bash
# Set test database URL (optional if using local docker defaults)
export TEST_DATABASE_URL="postgres://wallet_app:wallet_app_password@localhost:5432/wallet_test_db?sslmode=disable"

# Execute all tests with cache disabled
go test -v -count=1 ./...
```
*(On Windows PowerShell:)*
```powershell
$env:TEST_DATABASE_URL = "postgres://wallet_app:wallet_app_password@localhost:5432/wallet_test_db?sslmode=disable"
go test -v -count=1 ./...
```

### 10.2 Running Static Analysis & Linters
Verify static analysis and compile correctness:
```bash
go vet ./...
```

### 10.3 Running Concurrency & Stress Tests
Run high-concurrency double-spend and deadlock tests specifically:
```bash
go test -v -count=1 -run "TestTransferService_Concurrent" ./internal/service/...
```

### 10.4 Running Database Trigger & Migration Integrity Tests
Verify migration advisory locking, schema validation, and constraint triggers:
```bash
# Run migration runner, advisory locking, and database constraint trigger tests
go test -v -count=1 -run "TestDatabase|TestMigrations" ./...
```

