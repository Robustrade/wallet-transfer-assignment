# Wallet Transfer Platform: PostgreSQL Schema Design

## Table of Contents
1. [Overview](#overview)
2. [Core Design Principles](#core-design-principles)
3. [Schema Architecture](#schema-architecture)
4. [Design Decision Details](#design-decision-details)
5. [Scalability Strategy](#scalability-strategy)
6. [Data Integrity & Safety](#data-integrity--safety)
7. [Auditability & Compliance](#auditability--compliance)
8. [Operations & Monitoring](#operations--monitoring)

---

## Overview

The wallet transfer platform schema implements a **double-entry bookkeeping** system with idempotent request handling, optimistic locking for concurrency, and comprehensive audit trails.

### High-Level Entity Relationships

```
┌─────────────┐                    ┌──────────────┐
│   wallets   │◄──────────┐        │  transfers   │
│ ┌─────────┐ │           │        │ ┌──────────┐ │
│ │ balance │ │           └────────┤─│ from_id  │ │
│ │ version │ │  ┌────────┐        │ │ to_id    │ │
│ └─────────┘ │  │ledger_ │        │ │ amount   │ │
└─────────────┘  │entries │        │ │ status   │ │
                 └────────┘        └──────────┘ │
                      △                         │
                      │                         │
          ┌───────────┼─────────────────────────┘
          │           │
          │    ┌──────────────────────────┐
          │    │ idempotency_records      │
          │    │ ┌────────────────────┐   │
          └───►│ - idempotency_key   │   │
               │ - transfer_id       │   │
               │ - request_hash      │   │
               │ - status            │   │
               │ - response_json     │   │
               └────────────────────┘   │
               └──────────────────────────┘
```

### Data Model Characteristics

- **Immutability**: Ledger entries are immutable (INSERT-only); no updates/deletes
- **Double-Entry**: Every transfer creates exactly 1 DEBIT + 1 CREDIT entry
- **Conservation Law**: Total money in system = Σ(debits) = Σ(credits)
- **Auditability**: Complete transaction log with timestamps
- **Idempotency**: Safe retries via request key deduplication
- **Concurrency**: Optimistic locking via version column

---

## Core Design Principles

### 1. Use BIGINT for Monetary Values (Not FLOAT)

#### ❌ **Problem with FLOAT**
```sql
-- FLOAT has precision loss
SELECT 0.1::FLOAT8 + 0.2::FLOAT8;  -- Result: 0.30000000000000004 (not 0.3!)
SELECT 100.1::FLOAT8 * 100;         -- Result: 10010.000000000002 (not 10010!)
```

Floating-point arithmetic is fundamentally incompatible with money:
- IEEE 754 binary format cannot exactly represent decimal fractions like 0.1
- Rounding errors accumulate across many operations
- Comparisons become unreliable (0.3 <> 0.1 + 0.2)

#### ✅ **Solution: BIGINT in Smallest Currency Unit**
```sql
-- BIGINT: exact integer arithmetic
-- Convention: 1 unit = 1 cent (or smallest divisible unit)
-- Example: 100 cents = $1.00, 1000 cents = $10.00

SELECT 10::BIGINT + 20::BIGINT;     -- Result: 30 (exact!)
SELECT 100100::BIGINT / 100;        -- Result: 1001 (exact!)

-- At API layer: convert BIGINT ↔ decimal
-- Python: amount_cents = int(decimal_amount * 100)
// Go: amountCents := int64(decimalAmount * 100)
```

#### Scalability Impact
- **Small currency unit** = higher BIGINT range
  - 1 cent: BIGINT can handle up to 9.2 × 10^18 cents ≈ $92 × 10^15
  - Sufficient for global GDP scale (≈$100 × 10^12)
- **Multiple currencies**: application layer tracks currency separately
  - Schema agnostic to currency; assumes single unit at DB level

---

### 2. Store Balance Separately from Ledger

#### Why Two Representations?

| Aspect | Ledger | Balance |
|--------|--------|---------|
| **Query Performance** | O(N) table scan + aggregation | O(1) index lookup |
| **Consistency** | Single source of truth | Cache of ledger sum |
| **Updates** | Append-only (immutable) | Mutable (can be updated) |
| **Auditability** | Complete transaction log | Snapshot at point in time |
| **Correctness** | Guaranteed by constraints | Verified by reconciliation |

#### Pattern: Stored Aggregate + Transaction Log

**Example Flow:**
```sql
-- Initial state
wallets: id = A, balance = $100
ledger:  (empty)

-- Transfer $50 from A to B
BEGIN TRANSACTION

  -- 1. Verify balance and lock wallet
  SELECT balance FROM wallets WHERE id = A FOR UPDATE;  -- balance = 100

  -- 2. Update balance (fast)
  UPDATE wallets SET balance = balance - 50 WHERE id = A;

  -- 3. Create ledger entries (append)
  INSERT INTO ledger_entries (wallet_id, entry_type, amount) VALUES
    (A, 'DEBIT', 50),
    (B, 'CREDIT', 50);

COMMIT;

-- Final state
wallets: id = A, balance = 50
ledger:  sum of debits = 50, sum of credits = 50
```

#### Reconciliation: Periodic Verification
```sql
-- Check for discrepancies (run hourly)
SELECT * FROM v_wallet_reconciliation
WHERE stored_balance <> calculated_balance;

-- If discrepancy found:
-- 1. Debug (check ledger entries, audit logs)
-- 2. Alert operators
-- 3. Re-calculate balance from ledger (data recovery)
UPDATE wallets SET balance = calculated_balance WHERE discrepancy <> 0;
```

---

### 3. Preventing Negative Balances

#### Multiple Layers of Defense

```sql
-- Layer 1: CHECK Constraint (Database)
ALTER TABLE wallets ADD CONSTRAINT chk_wallet_balance_non_negative
    CHECK (balance >= 0);

-- Layer 2: Business Logic (Application)
// pseudocode
if wallet.balance < transfer.amount {
    return error "Insufficient funds"
}

// Only debit if validation passes
wallet.balance -= transfer.amount

-- Layer 3: Double-Entry Ledger (Invariant)
// Every DEBIT must have corresponding CREDIT
// Sum(DEBITS) = Sum(CREDITS)
// Wallet balance = Sum(CREDITS) - Sum(DEBITS)
// If balance < 0, means an extra DEBIT was created without CREDIT
```

#### Concurrency Safety

**Problem:** Multiple concurrent debit attempts on same wallet
```
Thread A: Check balance (100)        Thread B: Check balance (100)
Thread A: Debit 60 (leave 40)
Thread B: Debit 60 (leave 40)        <-- BUG: Total debit = 120, but balance = 100
```

**Solution: Row-Level Locking**
```sql
-- In application transaction:
BEGIN;
  SELECT * FROM wallets WHERE id = ? FOR UPDATE;  -- Acquire lock
  -- Now only one thread can proceed; others wait

  IF balance >= amount THEN
    UPDATE wallets SET balance = balance - amount;
  ELSE
    ROLLBACK;  -- Insufficient funds
  END IF;
COMMIT;
```

---

### 4. Supporting Auditability

#### Complete Transaction Log Design

The **ledger entries** table is the authoritative audit trail:

```sql
-- Every transfer creates exactly 2 ledger entries
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL,       -- Link to request
    wallet_id UUID NOT NULL,         -- Affected account
    entry_type ledger_entry_type,    -- DEBIT or CREDIT
    amount BIGINT,                   -- Amount moved
    created_at TIMESTAMPTZ           -- Exact timestamp
);

-- Unique constraint prevents duplicate entries
UNIQUE (transfer_id, entry_type)
```

#### Audit Queries

```sql
-- Who moved my money? (Transfer history)
SELECT t.id, t.from_wallet_id, t.to_wallet_id, t.amount, t.status, t.created_at
FROM transfers t
WHERE t.from_wallet_id = 'wallet-123' OR t.to_wallet_id = 'wallet-123'
ORDER BY t.created_at DESC;

-- Where did the $50 come from? (Trace back entry)
SELECT le.* FROM ledger_entries le
WHERE le.wallet_id = 'wallet-123' AND le.entry_type = 'CREDIT' AND le.amount = 5000
ORDER BY le.created_at DESC LIMIT 10;

-- Reconstruct balance at specific time
SELECT SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE -amount END)
FROM ledger_entries
WHERE wallet_id = 'wallet-123' AND created_at <= '2024-01-15 23:59:59'

-- Verify no double-spending (regulatory compliance)
SELECT wallet_id, SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE 0 END) as total_debits
FROM ledger_entries
GROUP BY wallet_id
HAVING total_debits > (SELECT balance FROM wallets WHERE id = wallet_id)
-- If query returns rows, an invariant is violated
```

#### Immutability for Legal Protection

```sql
-- Ledger entries are INSERT-only; never updated or deleted
CREATE TABLE ledger_entries (
    ...
    CONSTRAINT ... UNIQUE (transfer_id, entry_type)
);

-- Foreign key prevents deletion
CREATE TABLE transfers (
    ...
    CONSTRAINT fk_ledger_transfer
        FOREIGN KEY (...) REFERENCES ledger_entries(...)
        ON DELETE RESTRICT
);

-- Result: Once created, ledger entries cannot be altered
-- => Non-repudiable audit trail for legal proceedings
```

---

### 5. Supporting Future Scalability

#### Horizontal Scaling Patterns

##### **Pattern 1: Wallet Sharding**
As wallet count grows (millions → billions), shard wallets by ID:

```
Shard 0: Wallets with UUID in range [0x00...00, 0x40...00)
Shard 1: Wallets with UUID in range [0x40...00, 0x80...00)
Shard 2: Wallets with UUID in range [0x80...00, 0xC0...00)
Shard 3: Wallets with UUID in range [0xC0...00, 0xFF...FF]

-- Application layer determines shard:
shard = wallet_id.high_bytes() % num_shards
db_connection = get_shard_connection(shard)
```

**Schema Impact:**
- Each shard contains independent wallets, transfers, ledger entries
- Transfer between wallets on **different shards** requires distributed transaction
  - Use 2-phase commit (Raft consensus)
  - Or: idempotent application-layer retry + compensating transaction

**Current Schema Support:**
- UUID primary keys → no global sequence dependency
- Each shard has independent sequence space
- Foreign key constraints work within shard

##### **Pattern 2: Time-Based Partitioning (Ledger)**
Ledger entries grow monotonically; partition by date:

```sql
-- Partition ledger_entries by month
CREATE TABLE ledger_entries_2024_01 PARTITION OF ledger_entries
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

CREATE TABLE ledger_entries_2024_02 PARTITION OF ledger_entries
    FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');

-- Queries on old partitions can be archived/compressed
-- New transfers always insert into current partition
-- Index performance stays O(1) within partition
```

**Current Schema Support:**
- `created_at` column enables partitioning
- Partitioning is transparent to application (SQL queries unchanged)
- Reduces index size, improves query performance on large tables

##### **Pattern 3: CQRS (Command-Query Responsibility Segregation)**
Read-heavy analytics queries (e.g., reconciliation) slow down transactional writes:

```
Write DB (Transactional):
  ├─ wallets (balance updates)
  ├─ transfers (status updates)
  └─ ledger_entries (append-only)
         │
         └─► Stream (Kafka/EventLog)
               │
               └─► Read DB (Analytics)
                     ├─ wallet_daily_summary
                     ├─ transfer_stats
                     └─ ledger_analytics
```

**Current Schema Support:**
- Ledger is append-only → natural fit for event streaming
- Views (`v_wallet_reconciliation`, `v_transfer_stats`) can be materialized in read DB
- Application can publish events to Kafka on transfer completion

##### **Pattern 4: Archive Old Data**
Ledger can grow very large (millions of entries/day); archive to cold storage:

```sql
-- Archive ledger entries older than 2 years
CREATE TABLE ledger_entries_archive AS
SELECT * FROM ledger_entries
WHERE created_at < NOW() - INTERVAL '2 years';

DELETE FROM ledger_entries
WHERE created_at < NOW() - INTERVAL '2 years';

-- Move to S3/GCS for compliance retention
-- Keep hot ledger lean for fast queries
```

---

## Schema Architecture

### Core Tables

#### **wallets**
Fast read/write of wallet state
```sql
CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    version BIGINT NOT NULL,           -- Optimistic locking
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

#### **transfers**
Central audit trail of transfer requests
```sql
CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT UNIQUE,       -- Safe retries
    from_wallet_id UUID NOT NULL,
    to_wallet_id UUID NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    status transfer_status NOT NULL,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

#### **ledger_entries**
Immutable double-entry bookkeeping log
```sql
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL,
    wallet_id UUID NOT NULL,
    entry_type ledger_entry_type NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (transfer_id, entry_type)   -- Exactly 1 debit, 1 credit
);
```

#### **idempotency_records**
Enable safe retries via request deduplication
```sql
CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    transfer_id UUID,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL,              -- PENDING, COMPLETED, FAILED
    response_json JSONB NOT NULL,
    status_code INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
```

---

## Design Decision Details

### Constraint Types

| Constraint Type | Example | Enforced At | Recovery |
|---|---|---|---|
| **Primary Key** | `id UUID PRIMARY KEY` | Table level | Cannot insert duplicates |
| **UNIQUE** | `idempotency_key TEXT UNIQUE` | Index level | Prevents duplicate keys |
| **FOREIGN KEY** | `from_wallet_id REFERENCES wallets` | Database level | Referential integrity |
| **CHECK** | `CHECK (balance >= 0)` | Row level | Constraint violation error |
| **NOT NULL** | `balance BIGINT NOT NULL` | Column level | Constraint violation error |

### Index Strategy

```sql
-- Primary key automatically indexed (B-tree)
CREATE TABLE wallets (
    id UUID PRIMARY KEY,  -- Auto-indexed
    ...
);

-- Foreign key lookups benefit from index
CREATE INDEX idx_transfer_from_wallet ON transfers(from_wallet_id);

-- Time-range queries (reconciliation)
CREATE INDEX idx_transfer_created_at ON transfers(created_at DESC);

-- Filtered index: only active transfers
CREATE INDEX idx_transfer_pending
    ON transfers(status)
    WHERE status IN ('PENDING', 'FAILED');

-- Composite index: balance calculation
CREATE INDEX idx_ledger_wallet_type
    ON ledger_entries(wallet_id, entry_type)
    INCLUDE (amount);  -- Covering index
```

### Enum Types vs String

```sql
-- Enum (Type-safe, enforced at DB level)
CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
CREATE TABLE transfers (
    status transfer_status NOT NULL  -- Only valid values allowed
);

-- Advantage: Strict validation, smaller storage (1-4 bytes vs longer string)
-- Disadvantage: Schema change required to add new status

-- Alternative: String + Check Constraint
CREATE TABLE transfers (
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

-- Advantage: More flexible for future statuses
-- Disadvantage: Less type-safe, no compiler validation
```

**Decision:** Use ENUM for stability, known set of values.

---

## Scalability Strategy

### Performance Targets

```
Operation              Target        Current Capability
─────────────────────────────────────────────────────────
Get wallet balance     < 1ms         O(1) index lookup
Create transfer        < 10ms        2 rows insert + 2 ledger inserts
List transfers/wallet  < 100ms       Index on from/to_wallet_id
Reconcile wallet       < 5s          Single aggregate query (partition-aware)
Monthly audit report   < 60s         Time-based partition scan
```

### Query Performance Analysis

```sql
-- Fast: Index lookup
EXPLAIN ANALYZE
SELECT * FROM wallets WHERE id = 'uuid-123';
-- Index Scan using wallets_pkey (cost=0.29..0.31 rows=1)

-- Fast: Range query on index
EXPLAIN ANALYZE
SELECT * FROM transfers WHERE from_wallet_id = 'uuid-123' AND status = 'PROCESSED';
-- Bitmap Index Scan on idx_transfer_from_wallet (cost=...)

-- Potentially Slow: Aggregate on large table
EXPLAIN ANALYZE
SELECT SUM(amount) FROM ledger_entries WHERE wallet_id = 'uuid-123' AND entry_type = 'DEBIT';
-- Seq Scan on ledger_entries (if no index...)
-- Solution: Composite index idx_ledger_wallet_type

-- Mitigation: Time-based aggregation
-- Only sum ledger entries from last 90 days in fast path
-- Older data: pre-computed aggregate or archive
```

### Data Volume Estimates

```
Assumption: 1M wallets, 100 transfers/second

Daily transfers:    100 * 86400 = 8.64M transfers/day
Ledger entries:     8.64M * 2 = 17.28M entries/day
Monthly transfers:  ~260M
Monthly ledger:     ~520M entries

Year 1:
  ├─ Wallets:        1M rows × 100 bytes = 100 MB
  ├─ Transfers:      ~3B rows × 200 bytes = 600 GB
  ├─ Ledger:         ~6B rows × 120 bytes = 720 GB
  └─ Total:          ~1.3 TB

Year 5:
  ├─ Total:          ~6.5 TB (with archiving, ~500 GB hot)
  └─ Strategy: Archive to S3 after 2 years
```

### Index Space

```
Year 1:
  ├─ Primary keys:     ~200 GB (row IDs)
  ├─ Foreign keys:     ~300 GB (wallet IDs)
  ├─ Time indexes:     ~150 GB (created_at)
  └─ Total indexes:    ~650 GB

Mitigation:
  ├─ Partition tables (reduce index per partition)
  ├─ Archive old data
  ├─ Drop indexes on old partitions
  └─ Use columnar storage (Citus) for analytics
```

### Concurrency Patterns

```
Read-Heavy Scenario: Wallet balance checks
  ├─ Solution: SELECT balance FROM wallets WHERE id = ? (index lookup)
  ├─ No locks needed (dirty read is acceptable)
  └─ Scales to 10k+ QPS per instance

Write-Heavy Scenario: Parallel transfers on many wallets
  ├─ Solution: Lock only affected wallets (SELECT ... FOR UPDATE)
  ├─ No global lock; independent wallets proceed in parallel
  └─ Transfer A ↔ B doesn't block Transfer C ↔ D

Contention Scenario: Many transfers same wallet
  ├─ Problem: Sequential debit attempts queue at wallet lock
  ├─ Scaling limit: ~1000 transfers/sec on single wallet
  ├─ Mitigation 1: Wallet pooling (multiple wallet IDs per logical wallet)
  ├─ Mitigation 2: Rate limiting at application layer
  └─ Mitigation 3: Sharding (different wallet IDs → different DB instances)
```

---

## Data Integrity & Safety

### Constraint Enforcement

```sql
-- Example: Prevent self-transfer
CREATE TABLE transfers (
    ...
    CONSTRAINT chk_transfer_different_wallets
        CHECK (from_wallet_id <> to_wallet_id)
);

-- Violation attempt:
INSERT INTO transfers (..., from_wallet_id, to_wallet_id, ...)
VALUES (..., 'wallet-123', 'wallet-123', ...);
-- ERROR: new row for relation "transfers" violates check constraint

-- Example: Balance non-negativity
UPDATE wallets SET balance = -100 WHERE id = 'wallet-123';
-- ERROR: new row for relation "wallets" violates check constraint
```

### Atomicity of Operations

```sql
-- Entire transfer operation must be atomic
BEGIN TRANSACTION
  ISOLATION LEVEL SERIALIZABLE;

  -- 1. Lock wallets (prevent other transfers)
  SELECT * FROM wallets WHERE id = from_wallet_id FOR UPDATE;
  SELECT * FROM wallets WHERE id = to_wallet_id FOR UPDATE;

  -- 2. Verify balance
  IF balance < amount THEN
    ROLLBACK;
    RETURN error;
  END IF;

  -- 3. Update balances
  UPDATE wallets SET balance = balance - amount WHERE id = from_wallet_id;
  UPDATE wallets SET balance = balance + amount WHERE id = to_wallet_id;

  -- 4. Record in ledger (immutable log)
  INSERT INTO ledger_entries (...) VALUES (from_wallet_id, 'DEBIT', amount);
  INSERT INTO ledger_entries (...) VALUES (to_wallet_id, 'CREDIT', amount);

  -- 5. Update transfer status
  UPDATE transfers SET status = 'PROCESSED' WHERE id = transfer_id;

COMMIT;  -- All or nothing
```

### Foreign Key Integrity

```sql
-- Prevent orphaned references
CREATE TABLE transfers (
    ...
    from_wallet_id UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    ...
);

-- Prevent deleting wallet if transfers reference it
DELETE FROM wallets WHERE id = 'wallet-123';
-- ERROR: update or delete on table "wallets" violates foreign key constraint
-- "fk_transfers_from_wallet_id" on table "transfers"

-- Cascade delete for audit trail cleanup
CREATE TABLE idempotency_records (
    ...
    transfer_id UUID REFERENCES transfers(id) ON DELETE CASCADE
);

-- When transfer deleted, idempotency records also deleted
DELETE FROM transfers WHERE id = 'transfer-123';
-- Cascades to idempotency_records
```

---

## Auditability & Compliance

### Regulatory Requirements Supported

| Requirement | Implementation | Audit Query |
|---|---|---|
| **Transaction History** | Ledger entries immutable | `SELECT * FROM ledger_entries WHERE wallet_id = ? ORDER BY created_at` |
| **Non-Repudiation** | INSERT-only ledger + constraints | Ledger cannot be modified post-insertion |
| **Traceability** | Transfer ID links to ledger | `SELECT * FROM transfers WHERE id = ?` joins to ledger |
| **Timestamp Accuracy** | TIMESTAMPTZ default NOW() | `SELECT created_at FROM ledger_entries` |
| **Compliance Retention** | Archive strategy | 7-year archival, cold storage |
| **Reconciliation** | Views + periodic checks | `SELECT * FROM v_wallet_reconciliation WHERE balance_discrepancy <> 0` |

### Data Retention & Privacy

```sql
-- Support GDPR "right to delete" with audit trail preservation
-- Strategy: Soft delete (mark as deleted) instead of hard delete

-- Add to wallets table (optional):
ALTER TABLE wallets ADD COLUMN deleted_at TIMESTAMPTZ;

-- Query active wallets only:
SELECT * FROM wallets WHERE deleted_at IS NULL;

-- Audit trail preserved:
-- Ledger entries unchanged (immutable)
-- Historical transfers queryable
-- But wallet marked as inactive
```

---

## Operations & Monitoring

### Essential Monitoring Queries

```sql
-- 1. Wallet balance discrepancies (data integrity)
SELECT * FROM v_wallet_reconciliation
WHERE balance_discrepancy <> 0
ORDER BY ABS(balance_discrepancy) DESC;

-- 2. Stuck PENDING transfers (detect failures)
SELECT * FROM transfers
WHERE status = 'PENDING' AND created_at < NOW() - INTERVAL '5 minutes'
ORDER BY created_at ASC;

-- 3. Concurrent update conflicts (concurrency issues)
SELECT wallet_id, COUNT(*) as concurrent_attempts
FROM (
    SELECT wallet_id, created_at FROM transfers
    WHERE created_at > NOW() - INTERVAL '1 hour'
)
GROUP BY wallet_id
HAVING COUNT(*) > 100;

-- 4. Transfer success rate (operational health)
SELECT
    DATE_TRUNC('hour', created_at) as hour,
    status,
    COUNT(*) as count
FROM transfers
WHERE created_at > NOW() - INTERVAL '24 hours'
GROUP BY DATE_TRUNC('hour', created_at), status;

-- 5. Slow transfers (performance)
SELECT * FROM transfers
WHERE updated_at - created_at > INTERVAL '10 seconds'
ORDER BY updated_at - created_at DESC;
```

### Backup & Recovery Strategy

```sql
-- Full backup (hourly)
pg_dump --format=custom --file=backup_$(date +%Y%m%d_%H%M%S).pgdump

-- Point-in-time recovery
-- 1. Take WAL (Write-Ahead Log) backup
-- 2. If corruption detected:
//   pg_restore --until='2024-01-15 14:30:00' backup.pgdump > recovered.sql

-- Ledger-only backup (for compliance)
// Copy only ledger_entries (immutable) to separate archive DB
pg_dump --table=ledger_entries --format=plain > ledger_archive.sql
```

---

## Migration Path & Versioning

### Version History

```
V001: Initial schema
  ├─ wallets (with balance, version, timestamps)
  ├─ transfers (with idempotency_key, status)
  ├─ Enums: transfer_status
  └─ Indexes: from_wallet, to_wallet, idempotency_key

V002: Ledger & Idempotency
  ├─ ledger_entries (DEBIT/CREDIT entries)
  ├─ idempotency_records (for safe retries)
  ├─ Enums: ledger_entry_type
  ├─ Views: reconciliation, statistics
  └─ Indexes: wallet, transfer, created_at, pending
```

### Deployment Strategy

1. **Test in staging** with same data volume as production
2. **Apply migrations** in order (V001 → V002 → ...)
3. **Verify constraints** on existing data
4. **Monitor performance** after deployment
5. **Keep rollback plan** (can't undo immutable ledger entries)

---

## Summary: Key Takeaways

| Aspect | Design | Benefit |
|--------|--------|---------|
| **Monetary Amounts** | BIGINT in smallest unit | Exact arithmetic, no floating-point errors |
| **Balance Storage** | Separate from ledger | Fast queries + auditability |
| **Preventing Negatives** | Multi-layer (constraint + logic + ledger) | Safety at all levels |
| **Idempotency** | Unique key + request hash + status | Safe retries, failure recovery |
| **Double-Entry** | 2 ledger entries per transfer | Money conservation invariant |
| **Auditability** | INSERT-only ledger + cascading constraints | Non-repudiable audit trail |
| **Concurrency** | Optimistic locking + row-level locks | Scalable without global locks |
| **Scalability** | Sharding + time partitioning + CQRS | Handle billions of transfers |

---

## References & Further Reading

- PostgreSQL Documentation: https://www.postgresql.org/docs/
- ACID Properties: https://en.wikipedia.org/wiki/ACID
- Double-Entry Bookkeeping: https://en.wikipedia.org/wiki/Double-entry_bookkeeping
- Idempotent APIs: https://en.wikipedia.org/wiki/Idempotence
- Floating-Point Arithmetic Pitfalls: https://0.30000000000000004.com/
