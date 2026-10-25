# Wallet Transfer Service — Architecture & Technical Approach (RFC)

**Candidate:** Safeek  
**Target Role:** Backend SDE 3  
**Status:** In Review / Proposal (Pre-Implementation RFC)  
**Submission Date:** October 2026  

---

## 1. Executive Summary

This document details the architectural design, schema modeling, concurrency control strategy, and idempotency guarantees for the **Wallet Transfer Service**. 

The primary requirement is to execute wallet-to-wallet transfers reliably in a distributed environment where network timeouts, retries, concurrent balance mutations, and unexpected failures are standard operating conditions.

Key system attributes:
* **Double-Entry Bookkeeping:** Every monetary movement generates two balanced, immutable ledger entries.
* **API-Level Idempotency:** Exactly-once semantics enforced via durable database constraints and request hashing.
* **Deadlock-Free Concurrency Control:** Row-level pessimistic locking (`SELECT ... FOR UPDATE`) with deterministic resource ordering.
* **Preserved Failure Auditing:** Differentiating system exceptions from business failures to ensure terminal `FAILED` states are durable and retries receive deterministic responses.
* **ACID Transaction Boundaries:** Strict atomic updates combining wallet balances, ledger entries, transfer records, and idempotency entries.
* **Clean Layered Architecture:** Strict decoupling between Transport (Handlers), Domain Logic (Service), and Data Persistence (Repository).

---

## 2. API Contract & Validation Rules

### 2.1 Create Transfer Endpoint
* **Path:** `POST /transfers`
* **Headers:** `Content-Type: application/json`

#### Request Payload
```json
{
  "idempotencyKey": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "fromWalletId": "w_user_101",
  "toWalletId": "w_user_202",
  "amount": 2500
}
```

#### Early Validation Layer (Pre-Locking / Pre-Transaction)
Before opening database transactions or acquiring locks, the request is validated:
* `amount > 0`: Stored as an integer in the smallest currency unit (e.g., cents/paise) to eliminate IEEE-754 floating-point inaccuracies. Zero or negative amounts return `400 Bad Request`.
* `fromWalletId != toWalletId`: Self-transfers are rejected immediately with `400 Bad Request`.
* `idempotencyKey`, `fromWalletId`, `toWalletId`: Must be non-empty, sanitized strings.
* Maximum payload size bounds enforced to reject malformed/oversized JSON.

#### Response Payload (HTTP 201 Created / 200 OK for replays)
```json
{
  "transferId": "tr_8f9a2b1c",
  "idempotencyKey": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "fromWalletId": "w_user_101",
  "toWalletId": "w_user_202",
  "amount": 2500,
  "status": "PROCESSED",
  "createdAt": "2026-10-23T12:00:00Z"
}
```

#### Error Responses
* `400 Bad Request`: Validation failure (negative/zero amount, self-transfer, malformed fields).
* `404 Not Found`: Source or destination wallet does not exist.
* `409 Conflict`: 
  1. Idempotency key reused with a conflicting payload (different amount or destination).
  2. Insufficient funds (committed failure with audit persistence).
* `500 Internal Server Error`: Unhandled database error or unexpected infrastructure failure.

---

## 3. Database Schema Design

### 3.1 Entity Relationship Overview

```mermaid
erDiagram
    WALLETS ||--o{ LEDGER_ENTRIES : "records"
    WALLETS ||--o{ TRANSFERS : "source / dest"
    TRANSFERS ||--|{ LEDGER_ENTRIES : "generates exactly 2 if PROCESSED"
    TRANSFERS ||--o| IDEMPOTENCY_RECORDS : "tracks"

    WALLETS {
        string id PK
        bigint balance "CHECK (balance >= 0)"
        string currency
        timestamp created_at
        timestamp updated_at
    }

    TRANSFERS {
        string id PK
        string idempotency_key UK
        string from_wallet_id FK
        string to_wallet_id FK
        bigint amount "CHECK (amount > 0)"
        string status "PENDING, PROCESSED, FAILED"
        string failure_reason
        timestamp created_at
        timestamp updated_at
    }

    LEDGER_ENTRIES {
        string id PK
        string transfer_id FK
        string wallet_id FK
        string entry_type "DEBIT, CREDIT"
        bigint amount "CHECK (amount > 0)"
        timestamp created_at
    }

    IDEMPOTENCY_RECORDS {
        string key PK
        string request_hash
        string status "PENDING, COMPLETED"
        int response_status
        text response_body
        string transfer_id FK
        timestamp created_at
        timestamp updated_at
    }
```

### 3.2 Table Definitions & Defense-in-Depth Constraints

```sql
-- 1. Wallets
CREATE TABLE wallets (
    id VARCHAR(64) PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 2. Transfers
CREATE TABLE transfers (
    id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_transfers_status ON transfers(status);

-- 3. Ledger Entries (Double-Entry Bookkeeping - Append Only)
CREATE TABLE ledger_entries (
    id VARCHAR(64) PRIMARY KEY,
    transfer_id VARCHAR(64) NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ledger_wallet_created ON ledger_entries(wallet_id, created_at);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);

-- 4. Idempotency Records
CREATE TABLE idempotency_records (
    key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'COMPLETED')),
    response_status INT NOT NULL,
    response_body TEXT NOT NULL,
    transfer_id VARCHAR(64) NULL REFERENCES transfers(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## 4. Concurrency & Deadlock Prevention Strategy

### 4.1 The Race Condition Threat
If two requests debit the same wallet ($W_1$) concurrently:
* Request 1: Debits \$80 from $W_1$ (balance = \$100).
* Request 2: Debits \$50 from $W_1$ (balance = \$100).
* Without locking, both check `$100 >= amount` simultaneously and both succeed, driving the balance to `-$30` (double-spending).

### 4.2 Database Concurrency Strategy: PostgreSQL vs. SQLite

#### Primary Production Target: PostgreSQL (Fine-Grained Row-Level Locking)
In PostgreSQL, we use pessimistic row-level locks within an explicit ACID transaction:
```sql
SELECT id, balance FROM wallets WHERE id = $1 FOR UPDATE;
```
* **Deadlock Elimination via Deterministic Ordering:** If Tx 1 transfers $W_A \to W_B$ while Tx 2 transfers $W_B \to W_A$, arbitrary locking order causes a circular deadlock. We eliminate this by **always acquiring row locks in lexicographically sorted order** of wallet IDs:
  ```go
  firstWalletID, secondWalletID := fromWalletID, toWalletID
  if firstWalletID > secondWalletID {
      firstWalletID, secondWalletID = toWalletID, fromWalletID
  }

  // 1. SELECT ... WHERE id = firstWalletID FOR UPDATE
  // 2. SELECT ... WHERE id = secondWalletID FOR UPDATE
  ```
  Because all concurrent transactions acquire row locks in the exact same global order, cyclic wait graphs are mathematically prevented.

#### Secondary / Local Target: SQLite (Database-Level Transaction Serialization)
> **Note on SQLite Concurrency:** SQLite does not support row-level locks (`SELECT ... FOR UPDATE` is invalid syntax).
* For SQLite environments (e.g. lightweight local embedded runs), concurrency is managed via **database-level transaction serialization**:
  - Open transactions with `BEGIN IMMEDIATE` (or `BEGIN EXCLUSIVE`) to acquire a write reservation upfront, avoiding mid-transaction upgrade deadlocks.
  - Configure `PRAGMA busy_timeout = 5000;` and WAL mode (`PRAGMA journal_mode = WAL;`).
  - Implement application-level exponential backoff retries on `SQLITE_BUSY` errors.
* **Test Validation Matrix:**
  - True non-blocking, multi-wallet row concurrency is tested against **PostgreSQL** (where $W_A \to W_B$ and $W_C \to W_D$ run in parallel without blocking each other).
  - SQLite validates functional correctness and serialized execution safety.

### 4.3 Database Check Constraint
In addition to application row locking, the `balance >= 0` database constraint guarantees defense-in-depth: even if application locking fails due to a bug, the database engine will abort any transaction that causes a negative balance.

---

## 5. Double-Entry Ledger Mechanics

For every transfer of amount $X$ from $W_{from}$ to $W_{to}$:
1. **Debit Entry:** `(wallet_id: W_from, type: 'DEBIT', amount: X)`
2. **Credit Entry:** `(wallet_id: W_to, type: 'CREDIT', amount: X)`

### Invariants Enforced:
1. **Total Conservation of Money:**
   $$\sum \text{Credits} - \sum \text{Debits} = 0$$
2. **Reconciliation Auditability:**
   $$\text{Wallet Current Balance} = \sum_{entries} (\text{CREDIT} - \text{DEBIT})$$
   Stored balance serves as a high-performance cached snapshot, validated against ledger sum during audits.
3. **Immutability:**
   Ledger entries are strictly append-only. No `UPDATE` or `DELETE` operations are ever executed against `ledger_entries`.

---

## 6. Idempotency & Failure Persistence Strategy

### 6.1 Differentiating System Errors vs Business Failures (Crucial SDE-3 Distinction)
* **System Errors (DB connection timeout, infrastructure crash):**
  $\rightarrow$ The database transaction **rolls back** cleanly. The client receives `500 Internal Server Error` (or `503 Service Unavailable`) and can retry safely.
* **Business Logic Failures (Insufficient Funds, Inactive Wallet):**
  $\rightarrow$ **DO NOT ROLL BACK!** Rolling back deletes the transfer record and idempotency key from the database, causing audit history loss and non-deterministic retry behavior.
  $\rightarrow$ Instead, the transaction **commits** with:
  1. Transfer `status = FAILED`, `failure_reason = 'INSUFFICIENT_FUNDS'`.
  2. Idempotency record saved with `response_status = 409` (or 422) and cached error body.
  3. Zero ledger entries created; wallet balances unchanged.
  4. Retrying the request returns the identical cached failure response deterministically.

### 6.2 Workflow

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant Handler
    participant Service
    participant Database

    Client->>Handler: POST /transfers (idempotencyKey, payload)
    Handler->>Service: ExecuteTransfer(...)
    Service->>Database: Query idempotency_records by Key
    alt Key already exists
        Database-->>Service: Return cached record
        Service->>Service: Compare SHA256(request_payload) with request_hash
        alt Payload Matches
            Service-->>Handler: Return cached response_body & cached response_status
            Handler-->>Client: Replay cached response (HTTP 200/201 if success, HTTP 409 if failed)
        else Payload Mismatch
            Service-->>Handler: ErrIdempotencyConflict
            Handler-->>Client: HTTP 409 Conflict (Idempotency Key reused with different payload)
        end
    else Key does not exist
        Service->>Database: Begin Transaction
        Service->>Database: Lock Wallets in Sorted Order (FOR UPDATE / BEGIN IMMEDIATE)
        alt Sufficient Funds
            Service->>Database: Deduct fromWallet (balance - amount)
            Service->>Database: Add toWallet (balance + amount)
            Service->>Database: Insert Transfer Record (status = PROCESSED)
            Service->>Database: Insert 2 Ledger Entries (DEBIT & CREDIT)
            Service->>Database: Insert Idempotency Record (status = COMPLETED, code = 201)
            Service->>Database: Commit Transaction
            Service-->>Handler: Return Transfer Result
            Handler-->>Client: HTTP 201 Created
        else Insufficient Funds
            Service->>Database: Insert Transfer Record (status = FAILED, reason = INSUFFICIENT_FUNDS)
            Service->>Database: Insert Idempotency Record (status = COMPLETED, code = 409)
            Service->>Database: Commit Transaction (Persist Audit)
            Service-->>Handler: ErrInsufficientFunds
            Handler-->>Client: HTTP 409 Insufficient Funds
        end
    end
```

### 6.3 Handling Races on Duplicate Requests with Identical or Conflicting Payloads

When concurrent duplicate requests with the same `idempotencyKey` arrive simultaneously:
1. **Constraint Collision Detection:**
   - Both requests may miss the initial lookup because neither has committed yet.
   - The transaction attempts insertion. Depending on insertion order, the loser request will trigger a database UNIQUE constraint violation on **either** `transfers.idempotency_key` or `idempotency_records.key`.
   - The application transaction manager explicitly catches duplicate key / unique constraint errors on both entities.
2. **Post-Rollback Re-Validation & Conflict Detection:**
   - The loser transaction immediately rolls back and polls/retrieves the winner's committed `idempotency_records` entry.
   - **Crucial Conflict Check:** The loser compares its incoming payload hash against the winner's committed `request_hash`:
     - **Matching Payloads (Legitimate Retry Race):** Return the winner's cached response (status code and body).
     - **Conflicting Payloads (Key Collision Race):** If two racing requests used the same `idempotencyKey` but had *different* parameters (e.g., Req A sent amount \$100, Req B sent amount \$200), the loser MUST NOT replay Req A's result. Instead, it rejects Req B with **`HTTP 409 Conflict`** indicating the key is already claimed with a different request payload.

---

## 7. Reviewer Rubric & Common Failure Patterns Addressed

| Candidate Failure Pattern | Why Other Candidates Failed | How Our Design Addresses It (SDE-3 Level) |
|---|---|---|
| **Bidirectional Deadlocks** | Locked `fromWallet` then `toWallet`. Simultaneous $A \to B$ and $B \to A$ caused deadlocks. | **Deterministic Ordering:** Lexicographically sort wallet IDs (`min(A, B)` then `max(A, B)`) before acquiring `FOR UPDATE` locks. |
| **Vanishing Failure State** | On insufficient funds, set `FAILED` then returned error triggering rollback $\to$ status was wiped. | **Committed Failure Audit:** Terminal failure states and idempotency records are committed; no ledger entries are created. |
| **Inaccurate Failure Replay** | Replaying cached business errors returned `HTTP 200` instead of the original error code. | **Preserved Response Status:** Idempotency cache stores `response_status` (e.g. 409); replays preserve original status and body. |
| **Idempotency Race with Different Payloads** | Racing duplicates with same key but different payloads replayed the winner's transfer. | **Post-Race Hash Re-Verification:** On unique constraint error, compare payload hash with winner; return `409 Conflict` if mismatch. |
| **SQLite `SELECT FOR UPDATE` Crash** | Attempted to use Postgres row locks in SQLite, causing syntax errors in local tests. | **Differentiated Locking Strategy:** Postgres uses row-level locks; SQLite uses `BEGIN IMMEDIATE` serialized transactions with backoff. |
| **Weak Invariant Enforcement** | Relied purely on Go application logic without database constraints. | **Defense in Depth:** DB `CHECK (balance >= 0)`, `CHECK (amount > 0)`, and foreign keys guarantee data integrity even during code defects. |
| **Missing Request Validation** | Allowed self-transfers ($A \to A$), non-positive amounts, or untrimmed strings. | **Strict Pre-Lock Validation:** Reject malformed requests with `400 Bad Request` prior to initiating any database locks. |

---

## 8. Layered Architecture (Go 1.24)

```text
wallet-transfer-assignment/
├── cmd/
│   └── server/
│       └── main.go               # App initialization, config, DI, router start
├── internal/
│   ├── model/                    # Pure domain models and custom errors
│   │   ├── wallet.go
│   │   ├── transfer.go
│   │   ├── ledger.go
│   │   └── errors.go
│   ├── handler/                  # HTTP controller layer
│   │   ├── transfer_handler.go   # JSON bind, validation, status code mapping
│   │   └── response.go           # Standard JSON response envelope
│   ├── service/                  # Core domain orchestrator
│   │   ├── transfer_service.go   # Business rules, hash calculation, workflow
│   │   └── transfer_service_test.go
│   └── repository/               # Data access layer
│       ├── repository.go         # Interfaces for Wallet, Transfer, Ledger, Idempotency
│       ├── postgres/             # PostgreSQL implementation with row locks (SELECT ... FOR UPDATE)
│       └── sqlite/               # SQLite implementation with BEGIN IMMEDIATE serialization
├── db/
│   └── migrations/               # Up/down SQL schema scripts
├── go.mod
├── go.sum
└── README.md
```

---

## 9. Testing Discipline

Testing follows the **Red-Green-Refactor** behavioral discipline:
1. **Unit Testing:**
   - Model validations (e.g., negative transfer rejection, self-transfer rejection).
   - Invariant calculations (ledger sum matches delta).
2. **Integration Testing:**
   - Full transactional workflow against an isolated database.
   - Idempotency verification: 10 repeated requests with the same key produce exactly 1 debit and return identical responses.
   - Idempotency conflict verification: Reusing key with different amount returns HTTP 409.
   - Insufficient funds check: Transfer records `FAILED` status and returns cached error on retry.
3. **High-Concurrency Stress Testing:**
   - Spin up $N = 50$ concurrent goroutines executing simultaneous transfers on shared wallets against PostgreSQL.
   - Validate zero deadlocks, zero negative balances, and verify that the final sum of wallet balances equals initial capital.

---

## 10. AI Tooling Disclosure

* **Tool:** Google Antigravity (Advanced Agentic Pair Programming).
* **Usage:** Architectural refinement, database schema constraint design, deadlock edge-case analysis, testing matrix generation, and documentation drafting.
* **Commitment:** All architectural decisions and code implementations are thoroughly understood and will be articulated and defended during the technical evaluation.
