## 1. Overview
This service provides an ACID-compliant HTTP API for wallet-to-wallet transfers with distributed idempotency, double-entry ledger auditing, deterministic deadlock prevention, and consistent error handling. 

Language: Golang

Framework: Gin

Database: Postgres

---

## 2. Architecture & Layering

The codebase follows layered architectural principles:

cmd/server/          -> Application entrypoint & dependency injection wiring
internal/
  |-- handler/       -> HTTP routing, request decoding, error-to-status mapping
  |-- service/       -> Business orchestration, payload hashing, idempotency replay
  |-- repository/    -> Atomic transactions, row locking, SQL execution
  |-- model/         -> Domain entities, status checks, minor-unit conversions
  |-- apperror/      -> Strongly typed domain errors
  \-- config/        -> Environment configuration loading

---

## 3. Data Model

### wallets
| Column | Type | Constraints | Description |
| :--- | :--- | :--- | :--- |
| id | VARCHAR(64) | PRIMARY KEY | Unique wallet identifier |
| balance | BIGINT | NOT NULL CHECK (balance >= 0) | Balance in minor units (cents) |
| status | VARCHAR(32) | NOT NULL | ACTIVE, FROZEN, CLOSED, etc. |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT CURRENT_TIMESTAMP | Last update timestamp |

### transfers
| Column | Type | Constraints | Description |
| :--- | :--- | :--- | :--- |
| id | VARCHAR(64) | PRIMARY KEY | Transfer UUID |
| idempotency_key | VARCHAR(128) | NOT NULL UNIQUE | Client-provided idempotency key |
| from_wallet_id | VARCHAR(64) | NOT NULL REFERENCES wallets(id) | Source wallet |
| to_wallet_id | VARCHAR(64) | NOT NULL REFERENCES wallets(id) | Destination wallet |
| amount | BIGINT | NOT NULL CHECK (amount > 0) | Transfer amount in minor units |
| status | VARCHAR(32) | NOT NULL | PENDING, PROCESSED, FAILED |
| failure_reason | TEXT | NULL | Reason for rejection if failed |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT CURRENT_TIMESTAMP | Creation timestamp |

### ledger_entries
| Column | Type | Constraints | Description |
| :--- | :--- | :--- | :--- |
| id | BIGSERIAL | PRIMARY KEY | Ledger sequence ID |
| wallet_id | VARCHAR(64) | NOT NULL REFERENCES wallets(id) | Affected wallet |
| transfer_id | VARCHAR(64) | NOT NULL REFERENCES transfers(id) | Associated transfer |
| type | VARCHAR(16) | NOT NULL CHECK (type IN ('DEBIT','CREDIT')) | Entry type |
| amount | BIGINT | NOT NULL CHECK (amount > 0) | Amount changed |
| balance_before | BIGINT | NOT NULL | Balance snapshot before change |
| balance_after | BIGINT | NOT NULL | Balance snapshot after change |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT CURRENT_TIMESTAMP | Immutable timestamp |

### idempotency_records
| Column | Type | Constraints | Description |
| :--- | :--- | :--- | :--- |
| key | VARCHAR(128) | PRIMARY KEY | Idempotency key |
| request_hash | CHAR(64) | NOT NULL | SHA-256 fingerprint of request |
| status | VARCHAR(32) | NOT NULL | STARTED, COMPLETED, FAILED, EXPIRED |
| response_code | INTEGER | NULL | HTTP response status code |
| response_body | TEXT | NULL | Serialized JSON response or error body |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT CURRENT_TIMESTAMP | Initial acquisition timestamp |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT CURRENT_TIMESTAMP | Lease heartbeat/finalized timestamp |

---

## 4. Idempotency & Exactly-Once Semantics

To prevent double debits and ensure deterministic retries:

                      Client Request
                            |
                            v
              Compute SHA-256 Request Hash
                            |
                            v
        INSERT INTO idempotency_records ON CONFLICT DO NOTHING
                     /             \
       [Row Inserted]               [Row Already Existed]
             |                                |
             v                                v
       Acquired Lease                Inspect Existing Record
       (Status: STARTED)                      |
             |                    +-----------+-----------+
             |                    |                       |
             |             Hash Mismatch?          Status == COMPLETED / FAILED?
             |                    |                       |
             |           Return 400 Mismatch     Return Cached Code & Body
             |                                            |
             |                               [If Status == STARTED]
             |                                            |
             |                                   Age > LockTTL?
             |                                   /            \
             |                             [No]               [Yes]
             |                               |                  |
             |                       Return 409 In-Prog   Attempt Lease Takeover
             |                                            (FOR UPDATE NOWAIT)
             v                                                  |
    Execute Transfer                                     [Takeover Won]
   Inside Single SQL Tx                                         |
             |                                                  v
             \---------------------------------------> Execute Transfer

### Crash Recovery & Lease Takeovers
* If a worker crashes or encounters an unhandled timeout while holding a lease in STARTED, future requests hitting that key past lockTTL attempt a non-blocking SELECT ... FOR UPDATE NOWAIT.
* Lock Contention (55P03) Detection: If another active worker holds the row lock, Postgres returns error code 55P03. The repo recognizes this as active contention and safely returns ErrRequestInProgress (409 Conflict).
* If the row lock is acquired and the prior worker finished just before the check, the finalized result is returned. Otherwise, the lease is reclaimed via CAS update on updated_at.

---

## 5. Transaction Boundary & Concurrency Control

All balance adjustments, ledger writes, and status records execute within a single PostgreSQL transaction (READ COMMITTED).

                    [Begin Transaction]
                             |
            1. Lock idempotency_records (FOR UPDATE)
                             |
            2. Lexicographical Wallet Locking
               (SELECT ... WHERE id IN (A, B) ORDER BY id FOR UPDATE)
                             |
            3. Business Rule Validation
               |-- Are wallets ACTIVE/unrestricted?
               |-- Does source have sufficient balance?
               \-- Does dest balance + amount exceed math.MaxInt64?
                             |
              +--------------+--------------+
       [Rule Failed]                  [Checks Pass]
              |                              |
   INSERT transfers (FAILED)         UPDATE wallets (Debit source)
   UPDATE idempotency (FAILED)       UPDATE wallets (Credit dest)
   COMMIT & Return Domain Error      INSERT transfers (PROCESSED)
                                     INSERT ledger_entries (Debit & Credit)
                                     UPDATE idempotency (COMPLETED)
                                     COMMIT & Return 201 Created

### Deadlock Prevention
When two bidirectional transfers occur concurrently between the same pair of wallets (e.g., A -> B and B -> A), circular wait conditions can cause database deadlocks. The repository sorts the wallet IDs lexicographically before issuing the query:

    SELECT id, balance, status 
    FROM wallets 
    WHERE id IN ($1, $2) 
    ORDER BY id 
    FOR UPDATE;

Because both transactions always acquire locks in the identical order, deadlocks between concurrent transfers are prevented.

### Atomic Failure Auditing
Rejections due to business rules (e.g., insufficient balance, frozen account) do not trigger an unrecorded rollback. Instead, the transaction records the attempt in transfers (status = 'FAILED', with failure_reason) and finalizes the idempotency_records table atomically within the transaction before committing. This eliminates unlogged rejections and prevents idempotency keys from being stranded in STARTED.

---

## 6. Immutable Double-Entry Ledger

* Balances are never modified without creating corresponding immutable records in ledger_entries.
* Each successful transfer writes two complementary entries:
  1. A DEBIT entry for the source wallet.
  2. A CREDIT entry for the destination wallet.
* Every ledger row stores balance_before and balance_after, enabling full historical reconstruction and balance verification.

---

## 7. Arithmetic Safety & Representation

* All monetary amounts are represented as 64-bit signed integers (int64 / BIGINT) representing minor currency units (e.g., cents), avoiding floating-point rounding errors.
* Boundary checks ensure amounts are strictly positive (> 0).
* Destination wallet balances are checked against math.MaxInt64 - amount prior to addition to guard against integer overflow.
EOF