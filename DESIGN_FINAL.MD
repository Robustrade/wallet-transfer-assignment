Transfer flow

Request --> idempotency_records table --> 
lock wallet ids, insert into transfer table(Status:PENDING)
Update Wallets
Update ledger
Update transfer Status
Update idempotency_records status

# System Design Document: Wallet Transfer Service

## 1. Problem Statement

The objective is to implement a highly reliable, concurrent, and idempotent wallet-to-wallet transfer service using **Go**, **Gin**, and **PostgreSQL**.

Financial systems demand absolute data integrity. The core engineering challenges of this service include:

* Guarantees of **exactly-once processing** (idempotency) across network retries.
* Strict adherence to **double-entry ledger rules** where money is never created or destroyed.
* **Isolation control** to prevent race conditions (such as double-spending) when multiple transactions target the same wallets simultaneously.
* Prevention of **database deadlocks** caused by circular concurrent requests.

To maintain a clean separation of concerns, the service will be structured using a clean, layered architecture: **Handler Layer** (HTTP routing/validation) $\rightarrow$ **Service Layer** (orchestration/business rules) $\rightarrow$ **Repository Layer** (SQL queries/database transactions) $\rightarrow$ **Domain Models** (core entities/state transitions).

---

## 2. API Contract

### Create Transfer

* **Path:** `POST /transfers`
* **Headers:** `Content-Type: application/json`

#### Request Payload

```json
{
  "idempotencyKey": "unique-uuid-v4-string",
  "fromWalletId": "6c457313-9a3d-4c3e-8e56-111111111111",
  "toWalletId": "7d568424-8b4e-5d4f-9f67-222222222222",
  "amount": 150.50
}

```

#### Success Response (`200 OK` or `201 Created`)

```json
{"success":true,"data":{"transferId":"82de58ec-3b44-4789-bd17-af6ade6928e9","idempotencyKey":"abc12346","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100,"status":"PROCESSED","createdAt":"0001-01-01T00:00:00Z"}}


```

#### Error Responses

* `400 Bad Request`: Validation failures (e.g., negative amount, self-transfers, payload mismatches on repeated idempotency keys).
* `422 Unprocessable Entity`: Business logic failures (e.g., insufficient funds).
* `409 Conflict` / `202 Accepted`: Optional status if an identical idempotency key is concurrently evaluated in an uncommitted transaction (depending on client retry configuration).

---

## 3. Database Schema Design

The schema uses PostgreSQL constraints and data types optimized for financial records. Numeric fields use `NUMERIC(15, 4)` to eliminate floating-point rounding errors.

```sql
-- 1. Wallets Table
CREATE TABLE wallets (
    id  VARCHAR(255) PRIMARY KEY,
    balance NUMERIC(15, 4) NOT NULL DEFAULT 0.0000,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_positive_balance CHECK (balance >= 0)
);

-- 2. Transfers Table
CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id VARCHAR NOT NULL REFERENCES wallets(id),
    to_wallet_id  VARCHAR NOT NULL REFERENCES wallets(id),
    amount NUMERIC(15, 4) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_positive_amount CHECK (amount > 0),
    CONSTRAINT chk_no_self_transfer CHECK (from_wallet_id <> to_wallet_id)
);

-- 3. Ledger Entries Table
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id VARCHAR NOT NULL REFERENCES wallets(id),
    type VARCHAR(10) NOT NULL, -- 'DEBIT' or 'CREDIT'
    amount NUMERIC(15, 4) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_ledger_amount CHECK (amount > 0),
    CONSTRAINT chk_ledger_type CHECK (type IN ('DEBIT', 'CREDIT'))
);

-- 4. Idempotency Records Table
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL, -- SHA256 of payload to ensure data payload integrity
    response_code INT NOT NULL,
    response_body TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

```

---

## 4. Execution Workflow (Single Transaction Flow)

The absolute safest way to ensure consistency without dangling states is to execute the entire operations lifecycle inside a **single database transaction boundary**.

```text
[Client Request] 
       │
       ▼
 1. Gin Handler ──► Parse & Validate Payload (amount > 0, from != to)
       │
       ▼
 2. Begin DB Tx ──► START TRANSACTION;
       │
       ▼
 3. Idempotency ──► INSERT INTO idempotency_records ...
       │              ├── ON CONFLICT: Fetch cached response, ROLLBACK, return cached data.
       │              └── Success: Proceed.
       ▼
 4. Lock Rows   ──► SELECT * FROM wallets WHERE id IN (from, to) ORDER BY id FOR UPDATE;
       │              └── Note: Sorting IDs lexicographically prevents deadlocks.
       ▼
5. Insert Pending State  ──► INSERT INTO transfers (status = 'PENDING')
      
6. Verify balance  ──► Verify 'from' wallet balance >= amount.
       │              └── Failure: UPDATE transfer to 'FAILED', COMMIT, return 422.
       ▼
7. Apply Ledger──► INSERT INTO ledger_entries (DEBIT source, CREDIT destination)
       │
       ▼
8. Update Bal  ──► UPDATE wallets SET balance = balance +/- amount WHERE id = ...
       │
       ▼
9. Save State  ──► INSERT INTO transfers (status = 'PROCESSED')
       │
       ▼
 10. Cache Output──► UPDATE idempotency_records SET status = 200, body = [JSON]
       │
       ▼
11. Finalize    ──► COMMIT; (Postgres implicitly releases all row locks)

```

---

## 5. Idempotency & Retry Behavior

The service achieves exactly-once semantics by treating the `idempotency_key` as a strict application gatekeeper inside the transaction boundaries:

1. **Direct Atomicity:** The application attempts to write to `idempotency_records` at the start of the transaction.
2. **Handling Retries & Race Conditions:** If a network retry occurs while the original request is executing, the second request's `INSERT` statement will hit the unique index lock on `idempotency_key`. Postgres forces the second request to **block** and wait for the first transaction to complete.
* *If the first transaction commits:* The second request wakes up, throws a `Unique Key Violation`, catches the exception, reads the newly cached response from `idempotency_records`, and returns it instantly.
* *If the first transaction crashes:* The database drops the connection, automatically rolls back all changes, and frees the key. The second request wakes up, successfully inserts the key, and executes cleanly.


3. **Payload Tampering Protection:** The `request_hash` field stores a SHA-256 hash of the initial input payload parameters (`fromWalletId`, `toWalletId`, `amount`). If a client attempts to reuse a key with modified transaction data, the system flags a mismatch and returns a `400 Bad Request` instead of serving cached data.

---

## 6. Concurrency Safety & Consistency Expectations

### Pessimistic Row-Level Locking

To safely process concurrent requests changing wallet states, the application avoids optimistic locking or external distributed locks in favor of native PostgreSQL row locks:

```sql
SELECT id, balance FROM wallets WHERE id IN ($1, $2) ORDER BY id FOR UPDATE;

```

The `FOR UPDATE` modifier tells Postgres to lock those specific rows until the entire transaction ends. Any subsequent concurrent request attempting to access or modify those same wallets will form an orderly execution queue inside the database engine.

### Deadlock Prevention

Deadlocks occur if Request 1 locks Wallet A and waits for Wallet B, while Request 2 locks Wallet B and waits for Wallet A. The application completely eliminates this vulnerability by **sorting the wallet IDs lexicographically (alphabetically by UUID)** in Go before building the `SELECT ... FOR UPDATE` statement. Because every transaction always locks rows in the exact same sequence, a circular wait condition is structurally impossible.

### Dynamic Isolation Flow (Double-Debits)

If a user tries to spend $100 twice simultaneously with distinct keys, the first transaction to hit the lock checks the balance, validates it, debits the wallet, and commits. The second transaction wakes up from the lock queue, reads the newly reduced balance, fails the validation check, writes a `FAILED` transfer state, and rolls back cleanly without causing a negative balance or double-spend anomaly.

---

## 7. Failure Modes & Side Effects

### Business Validation Failures

* **Scenario:** Insufficient balance during execution.
* **Handling:** The transaction is **committed**, but with zero financial mutations. The system saves the transfer status as `FAILED` and stores a `422 Unprocessable Entity` response inside `idempotency_records`. Subsequent retries with the same key safely return the exact same structural validation error without putting load on system resources.

### Infrastructure/System Failures

* **Scenario:** Database timeout, network drop during execution, or Go application crash.
* **Handling:** The database connection terminates. PostgreSQL executes an immediate, atomic `ROLLBACK`. No balances are adjusted, no ledger entries are saved, and the idempotency record is wiped clean. This allows the client to re-attempt the transfer cleanly on a new network connection.

---

## 8. Design Decisions & Trade-offs

### Chosen Approach: Single Atomic Transaction

* **Pros:** Radical simplicity. Zero possibility of orphaned states (such as writing an idempotency key as `PENDING` but crashing before money moves, locking the user out indefinitely). Relies directly on the maturity of PostgreSQL ACID mechanics.
* **Cons:** Concurrent retries will briefly hang/block at the database level while waiting for the original transaction to commit, rather than immediately returning a `409 Conflict`. Given a targeted REST API response time, this brief block is a preferred trade-off over implementing asynchronous sweepers and cleanup crons.

### Real-World Scaling: The Hot Wallet / Merchant Contention Problem

While pessimistic row locking ensures absolute consistency, it introduces an architectural bottleneck if a single "merchant" wallet receives thousands of payments concurrently. All transactions would be forced into a single-threaded queue for that merchant row.

While out of scope for the current 3-5 hour project implementation, this hot-wallet problem can be resolved at scale via:

1. **Lock-Free Credits (Atomic Additions):** Credits only increase balance values and do not risk violating negative-balance invariants (`CHECK balance >= 0`). For destination wallets, row locks can be completely bypassed by applying isolated atomic queries:
```sql
UPDATE wallets SET balance = balance + :amount WHERE id = :dest_id;

```


2. **Asynchronous Ledger Aggregation:** Bypassing real-time wallet balance modifications entirely. The service can write exclusively to the insert-only `ledger_entries` table (which experiences zero lock contention) and compute or update cached wallet balances asynchronously via a highly scalable event pipeline (e.g., Kafka streaming workers).

---
