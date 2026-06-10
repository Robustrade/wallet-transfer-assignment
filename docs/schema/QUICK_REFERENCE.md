# PostgreSQL Schema: Quick Reference Guide

## Key Design Decisions

### 1. Why BIGINT for Monetary Values?

**❌ DON'T USE FLOAT**
```sql
-- Problem: Precision loss
SELECT 0.1::FLOAT + 0.2::FLOAT;  -- 0.30000000000000004 (wrong!)
SELECT (0.1 + 0.2)::FLOAT = 0.3;  -- false (dangerous!)
```

**✅ USE BIGINT**
```sql
-- Solution: Store in smallest unit (cents)
-- 100 = $1.00, 1000 = $10.00
SELECT 10::BIGINT + 20::BIGINT;  -- 30 (correct!)
SELECT (10 + 20)::BIGINT = 30;   -- true (reliable!)

-- Application converts: decimal ↔ BIGINT
// Go: cents := int64(dollars * 100)
// Python: cents = int(dollars * 100)
```

**Pros:** ✅ Exact arithmetic, ✅ Simple comparisons, ✅ No rounding errors  
**Cons:** ❌ Requires conversion at API layer

**Reference Range:**
- BIGINT range: -9,223,372,036,854,775,808 to +9,223,372,036,854,775,808
- In cents: ±$92,233,720,368,547.76 billion → handles global GDP scale

---

### 2. Why Store Balance Separately?

**Balance in Ledger Entries Only**
```
Pro:  ✅ Single source of truth
      ✅ Audit trail complete
Con:  ❌ SELECT SUM() slow on billions of rows (O(N))
      ❌ Reconciliation requires full table scan
```

**Balance in Separate Column**
```
Pro:  ✅ Fast lookup (O(1) index scan)
      ✅ Cache for read performance
      ✅ Ledger remains authoritative
Con:  ⚠️ Must keep synchronized
```

**Hybrid Solution (Current)**
```sql
wallets.balance      -- Cache of current state (read fast, update safe)
ledger_entries       -- Authoritative log (append-only, immutable)

-- Invariant: wallet.balance = SUM(ledger CREDITS) - SUM(ledger DEBITS)

-- Reconciliation: Periodic verification
SELECT * FROM v_wallet_reconciliation
WHERE balance_discrepancy <> 0;

-- Recovery: Restore balance from ledger
UPDATE wallets SET balance = calculated_balance
WHERE id IN (SELECT wallet_id FROM v_wallet_reconciliation WHERE discrepancy <> 0);
```

**Why This Works:**
1. **Fast reads:** Application queries `SELECT balance FROM wallets WHERE id = ?`
2. **Safe writes:** Transaction updates balance + creates ledger entries atomically
3. **Auditability:** Ledger is never modified (immutable)
4. **Recovery:** Can always rebuild balance from ledger

---

### 3. How to Prevent Negative Balances?

**Layer 1: Database Constraint**
```sql
CREATE TABLE wallets (
    balance BIGINT NOT NULL DEFAULT 0
    CONSTRAINT chk_wallet_balance_non_negative CHECK (balance >= 0)
);

-- Prevents direct SQL UPDATE
UPDATE wallets SET balance = -100 WHERE id = ?;
-- ERROR: violates check constraint
```

**Layer 2: Application Logic**
```go
// Pseudo-code: Debit operation
if wallet.Balance < transferAmount {
    return ErrInsufficientFunds  // Fail BEFORE updating
}
wallet.Balance -= transferAmount  // Only if check passes
```

**Layer 3: Double-Entry Ledger**
```sql
-- If somehow balance went negative, ledger would catch it
SELECT SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END)
FROM ledger_entries WHERE wallet_id = ?;

-- If result < 0, a DEBIT was created without CREDIT
-- Invariant violated = data corruption
```

**Concurrency Safety:**
```sql
-- Row-level lock prevents race condition
BEGIN TRANSACTION;
  SELECT * FROM wallets WHERE id = ? FOR UPDATE;  -- Lock this wallet
  -- Only one transaction can proceed; others WAIT

  IF balance >= amount THEN
    UPDATE wallets SET balance = balance - amount WHERE id = ?;
  ELSE
    ROLLBACK;  -- Insufficient funds
  END IF;
COMMIT;
```

---

### 4. How to Support Auditability?

**Immutable Ledger Entries**
```sql
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL,
    wallet_id UUID NOT NULL,
    entry_type ledger_entry_type NOT NULL,  -- DEBIT or CREDIT
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (transfer_id, entry_type)  -- Prevent duplicates
);

-- Foreign key prevents deletion
ALTER TABLE transfers ADD CONSTRAINT fk_ledger
    FOREIGN KEY (id) REFERENCES ledger_entries(transfer_id)
    ON DELETE RESTRICT;  -- Cannot delete transfer if ledger entries exist

-- Result: Once inserted, ledger entries CANNOT be changed
```

**Audit Queries**
```sql
-- "Show me all money movements for wallet X"
SELECT le.* FROM ledger_entries le
WHERE le.wallet_id = 'wallet-123'
ORDER BY le.created_at DESC;

-- "Trace where $50 came from"
SELECT le.*, t.* FROM ledger_entries le
JOIN transfers t ON le.transfer_id = t.id
WHERE le.wallet_id = 'wallet-123'
  AND le.entry_type = 'CREDIT'
  AND le.amount = 5000;

-- "Reconstruct balance at specific point in time"
SELECT SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE -amount END)
FROM ledger_entries
WHERE wallet_id = 'wallet-123'
  AND created_at <= '2024-01-15 23:59:59';
```

**Audit Logging (Optional V003)**
```sql
-- Track who made changes (for compliance)
CREATE TABLE audit_log (
    table_name TEXT,
    row_id TEXT,
    operation TEXT,  -- INSERT, UPDATE, DELETE
    changed_by TEXT,
    old_values JSONB,
    new_values JSONB,
    changed_at TIMESTAMPTZ
);

-- Example: "Who updated wallet balance?"
SELECT * FROM audit_log
WHERE table_name = 'wallets' AND operation = 'UPDATE'
ORDER BY changed_at DESC;
```

---

### 5. How to Support Future Scalability?

#### **Problem:** Billions of wallets, trillions of transfers

#### **Solution 1: Wallet Sharding**
```
Database Shard 0: Wallets UUID [0x00..00 to 0x40..00)
Database Shard 1: Wallets UUID [0x40..00 to 0x80..00)
Database Shard 2: Wallets UUID [0x80..00 to 0xC0..00)
Database Shard 3: Wallets UUID [0xC0..00 to 0xFF..FF]

Application:
  shard_id = wallet_uuid.hash() % num_shards
  db = get_shard_connection(shard_id)
  wallet = db.query("SELECT * FROM wallets WHERE id = ?")
```

**Current Schema Support:**
- ✅ UUID primary keys (no global sequence)
- ✅ Independent sequence space per shard
- ✅ Foreign keys work within shard

**Challenge:** Transfer between different shards → Distributed transaction

#### **Solution 2: Time-Based Ledger Partitioning**
```sql
-- Partition ledger by month (as data grows)
CREATE TABLE ledger_entries (
    ...
    created_at TIMESTAMPTZ
) PARTITION BY RANGE (DATE_TRUNC('month', created_at));

CREATE TABLE ledger_2024_01 PARTITION OF ledger_entries
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

CREATE TABLE ledger_2024_02 PARTITION OF ledger_entries
    FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');

-- Benefits:
-- - Smaller indexes (faster scans)
-- - Old partitions can be archived to S3
-- - Only current partition gets writes (hot)
-- - Queries still work transparently
```

**Current Schema Support:**
- ✅ `created_at` column enables partitioning
- ✅ Application unchanged (transparent)
- ✅ Archive strategy built-in

#### **Solution 3: Read Replicas**
```
Write DB (Transactional)
  └─ Streaming replication ──►  Read Replica 1 (Analytics)
                           ──►  Read Replica 2 (Reporting)

Application:
  - Write transfers to master
  - Query analytics from replicas
  - No locks on transaction path
```

#### **Solution 4: Event Sourcing**
```
Transactional DB              Event Stream (Kafka)
  │                                  │
  ├─ Transfer created ──────────────►├─ Event: TransferCreated
  ├─ Ledger entries added ───────────┤─ Event: LedgerEntriesCreated
  ├─ Balance updated ────────────────┤─ Event: BalanceUpdated
  │
  └─ Subscribers:
     ├─ Analytics DB (materialized views)
     ├─ Notification Service
     ├─ Compliance Dashboard
     └─ Archive Pipeline
```

---

## Common Performance Issues & Solutions

### Issue 1: Slow Balance Queries

```sql
-- ❌ SLOW: Aggregates entire ledger
SELECT SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END)
FROM ledger_entries
WHERE wallet_id = 'wallet-123';
-- Seq Scan on ledger_entries (billions of rows)

-- ✅ FAST: Read cached balance
SELECT balance FROM wallets WHERE id = 'wallet-123';
-- Index Scan using wallets_pkey (instant)

-- ✅ PERIODIC: Verify cached balance
SELECT * FROM v_wallet_reconciliation
WHERE wallet_id = 'wallet-123';  -- Check for discrepancies
```

### Issue 2: Concurrent Updates on Same Wallet

```sql
-- Problem: Multiple debits queue at wallet lock
Thread A: Debit $50 (lock acquired)
Thread B: Debit $30 (waiting for lock)
Thread C: Debit $20 (waiting for lock)

-- Sequential execution: ~100ms per transfer
-- Max throughput: ~10 transfers/second on single wallet

-- Solution 1: Multiple wallet IDs (pooling)
"wallet-123-shard-0", "wallet-123-shard-1", ..., "wallet-123-shard-9"
-- Now 10 transfers can proceed in parallel

-- Solution 2: Rate limiting (application layer)
if (recentTransferCount > threshold) {
    return ErrRateLimited
}

-- Solution 3: Different database (sharding)
If wallet_id % num_shards == 0: use db0
If wallet_id % num_shards == 1: use db1
```

### Issue 3: Large Ledger Table

```sql
-- Current: 1B rows = ~120 GB
-- Problem: Scans slow, indexes large, backups huge

-- Solution: Archive old data
DELETE FROM ledger_entries WHERE created_at < NOW() - INTERVAL '2 years';
-- Keep hot ledger (recent data) for fast queries

-- Archive to S3:
COPY ledger_entries TO 'ledger_backup_2022.csv';
-- For compliance: retain 7 years in cold storage

-- Restore if needed:
COPY ledger_entries FROM 'ledger_backup_2022.csv';
```

---

## Schema Checklist

- [x] **BIGINT for money** (not FLOAT)
- [x] **Balance separate from ledger** (fast reads + auditability)
- [x] **CHECK constraints** (prevent negative balances)
- [x] **Row-level locking** (concurrent safety)
- [x] **Double-entry ledger** (money conservation)
- [x] **Immutable ledger entries** (audit trail)
- [x] **Idempotency key** (safe retries)
- [x] **Status tracking** (failure recovery)
- [x] **Timestamps** (audit trail)
- [x] **Indexes** (query performance)
- [x] **Foreign keys** (referential integrity)
- [x] **Triggers** (updated_at automation)
- [x] **Views** (reconciliation)

---

## Quick Queries

```sql
-- Get wallet balance
SELECT balance FROM wallets WHERE id = 'wallet-123';

-- List transfers for wallet
SELECT * FROM transfers
WHERE from_wallet_id = 'wallet-123' OR to_wallet_id = 'wallet-123'
ORDER BY created_at DESC;

-- Get ledger entries for wallet
SELECT * FROM ledger_entries
WHERE wallet_id = 'wallet-123'
ORDER BY created_at DESC;

-- Check reconciliation
SELECT * FROM v_wallet_reconciliation
WHERE balance_discrepancy <> 0;

-- Get transfer statistics (today)
SELECT status, COUNT(*) FROM transfers
WHERE created_at >= CURRENT_DATE
GROUP BY status;

-- List stuck transfers (pending > 5 min)
SELECT * FROM transfers
WHERE status = 'PENDING' AND created_at < NOW() - INTERVAL '5 minutes';

-- Verify ledger balance
SELECT
    SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE 0 END) as total_debits,
    SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE 0 END) as total_credits
FROM ledger_entries;
```

---

## Resources

- **Schema Design Details:** [SCHEMA_DESIGN.md](./SCHEMA_DESIGN.md)
- **Migration Guide:** [README.md](./README.md)
- **PostgreSQL Docs:** https://www.postgresql.org/docs/
- **ACID Properties:** https://en.wikipedia.org/wiki/ACID
- **Double-Entry Bookkeeping:** https://en.wikipedia.org/wiki/Double-entry_bookkeeping
- **Floating-Point Gotchas:** https://0.30000000000000004.com/
