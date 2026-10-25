# Architecture & Technical Approach: Wallet Transfer Service

## 1. Overview & Core Philosophy
The objective is to implement a high-integrity financial wallet engine handling wallet creation and inter-wallet transfers in strict accordance with the `ASSIGNMENT.md` contract:
* **Strong ACID Guarantees**: Transfer operations must be atomic (either debits and credits commit together or fail completely).
* **Double-Entry Ledger**: Balances are never modified in isolation without an accompanying immutable trail of debits and credits.
* **Strict Overdraft & Double-Spend Protection**: Under high concurrency, an account balance must never dip negative, and race conditions must not duplicate payouts.
* **Payload-Driven Idempotency**: Exactly-once execution semantics keyed off `idempotencyKey` present in the request body.

---

## 2. API Contract & Entity Naming Alignment

To ensure zero impedance mismatch with the test runner and grader requirements, all entities and API schemas match the assignment specification directly:

### `POST /transfers` Request Payload
```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}

```

* **Terminology**: The primary entity is `wallet` (`wallet_id`, `from_wallet_id`, `to_wallet_id`).
* **Key Extraction**: `idempotencyKey` is parsed directly from the JSON body (with optional header fallback support).
* **Currency / Precision**: Amounts are represented in integer minor units (cents). Floating-point arithmetic is strictly prohibited.

---

## 3. Storage Engine Choice: SQLite

While PostgreSQL is standard for multi-node deployments with row-level pessimistic locking (`FOR UPDATE`), **SQLite was chosen for this assignment**:

1. **Frictionless Local & CI Evaluation**: Reviewers can execute `go test ./...` immediately without provisioning Docker containers, external daemons, or managing network configurations.
2. **Pure-Go Driver (`modernc.org/sqlite`)**: Avoids CGo build dependencies, ensuring frictionless compilation on macOS, Linux, and Windows.
3. **Strict ACID Concurrency Tuning**:
    * `PRAGMA journal_mode = WAL;`: Enables concurrent readers without blocking write transactions.
    * `PRAGMA busy_timeout = 5000;`: Backs off and retries automatically for up to 5 seconds to eliminate transient `SQLITE_BUSY` contention.
    * `PRAGMA foreign_keys = ON;`: Enforces relational integrity across wallets, transfers, and ledger records.

---

## 4. Database Schema & Ledger Design

### Tables

* **`wallets`**:
    * `id` (TEXT PRIMARY KEY)
    * `user_id` (TEXT NOT NULL)
    * `currency` (TEXT NOT NULL)
    * `balance` (INTEGER NOT NULL DEFAULT 0)
    * `created_at` (DATETIME NOT NULL)
    * `updated_at` (DATETIME NOT NULL)
    * `CONSTRAINT chk_wallet_balance CHECK (balance >= 0)`
* **`transfers`**:
    * `id` (TEXT PRIMARY KEY)
    * `idempotency_key` (TEXT NOT NULL UNIQUE)
    * `from_wallet_id` (TEXT NOT NULL REFERENCES wallets(id))
    * `to_wallet_id` (TEXT NOT NULL REFERENCES wallets(id))
    * `amount` (INTEGER NOT NULL CHECK (amount > 0))
    * `status` (TEXT NOT NULL CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED')))
    * `created_at` (DATETIME NOT NULL)
    * `CONSTRAINT chk_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)`
* **`ledger_entries`**:
    * `id` (TEXT PRIMARY KEY)
    * `transfer_id` (TEXT NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT)
    * `wallet_id` (TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT)
    * `amount` (INTEGER NOT NULL CHECK (amount > 0))
    * `direction` (TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')))
    * `created_at` (DATETIME NOT NULL)
* **`idempotency_records`**:
    * `key` (TEXT PRIMARY KEY)
    * `request_hash` (TEXT NOT NULL)
    * `status` (TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'RESOLVED', 'FAILED')))
    * `response_code` (INTEGER)
    * `response_body` (TEXT)
    * `created_at` (DATETIME NOT NULL)
    * `locked_until` (DATETIME NOT NULL)

---

## 5. Concurrency, Transaction Boundaries & Idempotency Resolution

### SQLite Concurrency Model

SQLite utilizes database/file-level locking. Mutating transactions are initiated with `BEGIN IMMEDIATE` to acquire write reservation upfront, eliminating mid-transaction lock escalation deadlocks.

Additionally, wallet IDs are sorted deterministically (`min(fromWalletId, toWalletId)` then `max(...)`) prior to executing updates to ensure architectural consistency with row-level locking conventions.

### In-Flight Concurrency & Recovery Protocol

To avoid race conditions and stuck `IN_PROGRESS` locks:

```
                      Client Request
                             │
            Compute request_hash = SHA256(body)
                             │
              BEGIN IMMEDIATE (Write Tx 1)
                             │
                   Look up idempotencyKey
                             │
     ┌───────────────────────┼───────────────────────┐
     │ Found & RESOLVED      │ Found & IN_PROGRESS   │ Not Found or Expired
     ▼                       ▼                       ▼
Verify payload hash:    Check locked_until:     INSERT INTO idempotency_records
- Match: Return cached  - Not expired:          (key, hash, 'IN_PROGRESS', locked_until=now+15s)
  response (200/201)      Return 409 Conflict   ON CONFLICT(key) DO UPDATE...
- Mismatch: Return 422  - Expired (crash):      COMMIT Tx 1 (Reservation saved)
  Unprocessable           Take over reservation           │
                                                          ▼
                                                BEGIN IMMEDIATE (Tx 2: Transfer)
                                                          │
                                                - Verify balances & currencies
                                                - Debit fromWallet, Credit toWallet
                                                - INSERT transfers & ledger_entries
                                                - UPDATE idempotency_records -> 'RESOLVED'
                                                - COMMIT Tx 2
                                                          │
                                                [On Failure/Rollback]:
                                                UPDATE idempotency_records -> 'FAILED'
                                                (or delete row to free key for clean retry)

```

1. **Transaction Boundary Separation**:
    * The reservation marker is committed in **Transaction 1** (`IN_PROGRESS` with a short lease: `locked_until = datetime('now', '+15 seconds')`). This makes in-flight requests immediately observable to concurrent threads.
    * If a duplicate request arrives while another thread is processing, it observes `IN_PROGRESS` and receives `409 Conflict`.
2. **Recovery from Failures**:
    * **Validation/Business Rejections (e.g., Insufficient Balance)**: The service sets status to `FAILED` or deletes the in-flight record before bubbling the error, ensuring the client can retry cleanly.
    * **Service Crash / Abrupt Termination**: If the process crashes while `IN_PROGRESS`, subsequent retries compare `locked_until` against current time. If `now() > locked_until`, the expired reservation is reclaimed.
3. **Payload Integrity**:
    * If an existing key has a different SHA-256 payload hash, the server immediately returns `422 Unprocessable Entity` to reject mismatched replays.



---

## 6. Implementation Milestones

* **Milestone 1**: SQLite schema migrations (`wallets`, `transfers`, `ledger_entries`, `idempotency_records`), pure-Go driver setup, and domain models.
* **Milestone 2**: Repository layer implementing `BEGIN IMMEDIATE` transactions, deterministic ID ordering, double-entry ledger persistence, and two-phase idempotency reservation.
* **Milestone 3**: Domain/Service logic handling payload hash verification, balance validation, and currency matching.
* **Milestone 4**: HTTP routing matching the assignment contract (`POST /transfers`, `POST /wallets`, `GET /wallets/{id}`), payload parsing, and error code translation.
* **Milestone 5**: Integration test suite:
    * Parallel race conditions against single wallets proving non-negative balance guarantees.
    * Bidirectional concurrent transfers verifying absence of deadlock.
    * Concurrent transfer replays verifying exactly-once execution and `409 Conflict` / cached replay responses.