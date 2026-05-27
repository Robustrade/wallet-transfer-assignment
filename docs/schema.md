# Database Schema Design

## Overview

This document describes the database schema for the wallet transfer service. The design implements production-grade patterns for:
- **Idempotency** - Atomic duplicate prevention using database constraints
- **Concurrency** - Safe concurrent transfers with row-level locking
- **Double-entry ledger** - Immutable audit trail
- **Data integrity** - Constraints at the database level

---

## Tables

### 1. wallets

Stores wallet information and current balances.

```sql
CREATE TABLE wallets (
    id VARCHAR(255) PRIMARY KEY,
    balance DECIMAL(19, 4) NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT positive_balance CHECK (balance >= 0)
);

CREATE INDEX idx_wallets_balance ON wallets(balance);
```

**Columns:**
- `id` - Unique wallet identifier
- `balance` - Current wallet balance (stored for performance)
- `created_at` - Wallet creation timestamp
- `updated_at` - Last modification timestamp

**Constraints:**
- `positive_balance` - Prevents negative balances (CHECK constraint)

**Indexes:**
- `idx_wallets_balance` - Optimize balance queries

**Design Decisions:**
- Using **stored balance** instead of deriving from ledger for O(1) read/write performance
- Ledger remains the source of truth for auditing
- Periodic reconciliation should validate balance = ledger sum
- This is the standard approach used by Stripe, Square, etc.

---

### 2. transfers

Tracks all transfer requests and their lifecycle.

```sql
CREATE TABLE transfers (
    id VARCHAR(255) PRIMARY KEY,
    from_wallet_id VARCHAR(255) NOT NULL,
    to_wallet_id VARCHAR(255) NOT NULL,
    amount DECIMAL(19, 4) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (from_wallet_id) REFERENCES wallets(id),
    FOREIGN KEY (to_wallet_id) REFERENCES wallets(id),
    CONSTRAINT positive_amount CHECK (amount > 0),
    CONSTRAINT valid_status CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT different_wallets CHECK (from_wallet_id != to_wallet_id)
);

CREATE INDEX idx_transfers_status ON transfers(status);
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
```

**Columns:**
- `id` - Unique transfer identifier
- `from_wallet_id` - Source wallet (foreign key)
- `to_wallet_id` - Destination wallet (foreign key)
- `amount` - Transfer amount
- `status` - Transfer state (PENDING, PROCESSED, FAILED)
- `created_at` - Transfer creation timestamp
- `updated_at` - Last status update timestamp

**Constraints:**
- `positive_amount` - Ensures amount > 0
- `valid_status` - Only allows valid status values
- `different_wallets` - Prevents self-transfers

**Indexes:**
- `idx_transfers_status` - Query by status
- `idx_transfers_from_wallet` - Query sender's transfers
- `idx_transfers_to_wallet` - Query recipient's transfers

**State Machine:**
```
PENDING → PROCESSED  (success)
PENDING → FAILED     (validation error, insufficient balance)

Invalid transitions (must prevent):
PROCESSED → PENDING
PROCESSED → FAILED
FAILED → PROCESSED
```

---

### 3. ledger_entries

Immutable double-entry ledger for all transfers. **APPEND-ONLY**.

```sql
CREATE TABLE ledger_entries (
    id VARCHAR(255) PRIMARY KEY,
    wallet_id VARCHAR(255) NOT NULL,
    transfer_id VARCHAR(255) NOT NULL,
    entry_type VARCHAR(10) NOT NULL,
    amount DECIMAL(19, 4) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (wallet_id) REFERENCES wallets(id),
    FOREIGN KEY (transfer_id) REFERENCES transfers(id),
    CONSTRAINT valid_entry_type CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT positive_amount CHECK (amount > 0)
);

CREATE INDEX idx_ledger_wallet ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);
```

**Columns:**
- `id` - Unique entry identifier
- `wallet_id` - Wallet affected by this entry
- `transfer_id` - Transfer that created this entry
- `entry_type` - 'DEBIT' or 'CREDIT'
- `amount` - Entry amount (always positive)
- `created_at` - Entry creation timestamp

**Constraints:**
- `valid_entry_type` - Only DEBIT or CREDIT allowed
- `positive_amount` - Amount must be positive

**Indexes:**
- `idx_ledger_wallet` - Query entries by wallet
- `idx_ledger_transfer` - Query entries by transfer

**Double-Entry Guarantees:**
Every transfer produces exactly 2 entries:
```
| entry_id | wallet_id | transfer_id | entry_type | amount |
|----------|-----------|-------------|------------|--------|
| E1       | wallet_1  | T1          | DEBIT      | 100    |
| E2       | wallet_2  | T1          | CREDIT     | 100    |
```

**CRITICAL: Immutability**
- This table is **APPEND-ONLY**
- **NEVER UPDATE** or **DELETE** entries
- For corrections, create compensating entries via a reversal transfer
- This preserves complete audit trail

**Ledger Integrity:**
```sql
-- Total debits must equal total credits
SELECT SUM(CASE
    WHEN entry_type = 'CREDIT' THEN amount
    WHEN entry_type = 'DEBIT' THEN -amount
END) as balance
FROM ledger_entries;
-- Must equal 0
```

---

### 4. idempotency_records

Atomic duplicate prevention with request validation. **PRODUCTION-GRADE**.

```sql
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,  -- UNIQUE prevents race conditions
    request_hash VARCHAR(64) NOT NULL,         -- SHA256 for payload validation
    status VARCHAR(20) NOT NULL,               -- PROCESSING, COMPLETED, FAILED
    transfer_id VARCHAR(255),                  -- NULL until transfer created
    response_data JSONB,                       -- Cached response (PostgreSQL)
    -- response_data TEXT,                     -- Use TEXT for SQLite
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (transfer_id) REFERENCES transfers(id),
    CONSTRAINT valid_status CHECK (status IN ('PROCESSING', 'COMPLETED', 'FAILED'))
);

CREATE INDEX idx_idempotency_transfer ON idempotency_records(transfer_id);
CREATE INDEX idx_idempotency_status ON idempotency_records(status);
```

**Columns:**
- `idempotency_key` - Client-provided idempotency key (PRIMARY KEY)
- `request_hash` - SHA256 hash of request payload (fromWalletId + toWalletId + amount)
- `status` - Request processing status
- `transfer_id` - Created transfer ID (NULL until transfer is created)
- `response_data` - Cached response for duplicate requests (JSONB for PostgreSQL, TEXT for SQLite)
- `created_at` - Record creation timestamp
- `updated_at` - Last status update timestamp

**Constraints:**
- `PRIMARY KEY` on `idempotency_key` - **CRITICAL**: Prevents race conditions
- `valid_status` - Only allows PROCESSING, COMPLETED, FAILED

**Indexes:**
- `idx_idempotency_transfer` - Lookup by transfer
- `idx_idempotency_status` - Query by status

**Status State Machine:**
```
PROCESSING → COMPLETED  (transfer succeeded)
PROCESSING → FAILED     (transfer failed after processing attempt)

Retry Behavior:
- PROCESSING (< 30s): Return 409 "Request still processing, try again later"
- PROCESSING (> 30s): Return 409 "Original request timed out, use new idempotency key"
- COMPLETED: Return cached response (200 OK)
- FAILED: Return cached error (400/409)
```

**Important Notes:**
- Validation errors (wallet not found, insufficient balance) are NOT cached
- Only actual transfer attempts (COMPLETED/FAILED) are cached
- This allows clients to retry after fixing validation issues (e.g., depositing funds)

**Race Condition Fix:**
The PRIMARY KEY constraint prevents duplicate concurrent requests:

```sql
-- INSIDE TRANSACTION
INSERT INTO idempotency_records
  (idempotency_key, request_hash, status)
VALUES (?, ?, 'PROCESSING')
ON CONFLICT (idempotency_key) DO NOTHING;

-- If affected_rows = 0, record already exists (duplicate)
```

**Request Hash Validation:**
```go
// Generate hash from request fields
requestHash := SHA256(fromWalletId + toWalletId + amount)

// On duplicate key
if existingRecord.requestHash != requestHash {
    // Same key, different payload - client error
    return 409 Conflict, "Idempotency key reused with different payload"
}
```

This prevents clients from accidentally reusing idempotency keys with different transfer details.

---

## Entity Relationship Diagram

```
┌─────────────────┐
│     wallets     │
│─────────────────│
│ id (PK)         │
│ balance         │
│ created_at      │
│ updated_at      │
└────────┬────────┘
         │
         │ 1:N
         │
    ┌────┴──────────────────┐
    │                       │
    ▼                       ▼
┌─────────────────┐   ┌──────────────────┐
│   transfers     │   │  ledger_entries  │
│─────────────────│   │──────────────────│
│ id (PK)         │◄──│ id (PK)          │
│ from_wallet_id  │   │ wallet_id (FK)   │
│ to_wallet_id    │   │ transfer_id (FK) │
│ amount          │   │ entry_type       │
│ status          │   │ amount           │
│ created_at      │   │ created_at       │
│ updated_at      │   └──────────────────┘
└────────┬────────┘
         │
         │ 1:1
         │
         ▼
┌───────────────────────┐
│ idempotency_records   │
│───────────────────────│
│ idempotency_key (PK)  │
│ request_hash          │
│ status                │
│ transfer_id (FK)      │
│ response_data         │
│ created_at            │
│ updated_at            │
└───────────────────────┘
```

**Relationships:**
- `wallets` 1:N `transfers` (from_wallet_id)
- `wallets` 1:N `transfers` (to_wallet_id)
- `wallets` 1:N `ledger_entries`
- `transfers` 1:N `ledger_entries` (exactly 2 per transfer)
- `transfers` 1:1 `idempotency_records`

---

## Concurrency Strategy

### Row-Level Locking
```sql
-- Lock wallets in deterministic order (prevents deadlocks)
SELECT * FROM wallets WHERE id = ? FOR UPDATE;
```

### Lock Ordering
Always lock wallets in sorted order:
```go
walletIds := []string{fromWalletId, toWalletId}
sort.Strings(walletIds)  // Lexicographic order

for _, id := range walletIds {
    // SELECT ... FOR UPDATE in order
}
```

This prevents deadlocks when:
- Transfer A→B happens at same time as B→A
- Both transactions try to lock different wallets first

### Atomic Balance Updates
```sql
-- Never do SELECT-then-UPDATE
-- ❌ WRONG
SELECT balance FROM wallets WHERE id = ?;
UPDATE wallets SET balance = new_balance WHERE id = ?;

-- ✅ CORRECT - Atomic operation
UPDATE wallets
SET balance = balance - ?
WHERE id = ? AND balance >= ?;

-- Verify affected_rows = 1
```

---

## Transaction Isolation

**Level: READ COMMITTED**

Why READ COMMITTED is sufficient:
- Row-level locks already prevent concurrent modifications
- Repeatable Read / Serializable adds overhead without benefit
- Standard for PostgreSQL
- Used by production payment systems

**Transaction Scope:**
All operations in a single transaction:
1. Idempotency claim (INSERT ON CONFLICT)
2. Transfer creation
3. Ledger entries creation
4. Balance updates
5. Status updates
6. Response caching

**Crash Recovery:**
- If crash before COMMIT → automatic rollback
- No partial transfers possible
- Database guarantees atomicity

---

## Data Integrity

### Database-Level Constraints
1. **UNIQUE** on `idempotency_records.idempotency_key` - Prevents duplicates
2. **CHECK** constraints on amounts - Ensures positive values
3. **CHECK** constraints on statuses - Enforces valid state values
4. **FOREIGN KEYS** - Referential integrity
5. **NOT NULL** on critical fields - Prevents invalid data

### Application-Level Guarantees
1. Exactly 2 ledger entries per transfer (1 DEBIT + 1 CREDIT)
2. Equal amounts in both entries
3. Ledger balances to zero (SUM(DEBIT) = SUM(CREDIT))
4. Immutable ledger (no UPDATE/DELETE)

---

## Performance Considerations

### Indexes
All foreign keys have indexes for:
- Fast joins
- Fast lookups
- Query optimization

### Balance Strategy
**Stored balance** provides:
- O(1) read performance
- O(1) write performance
- Scalable to millions of wallets

**Trade-off:**
- Must reconcile with ledger periodically
- Ledger remains audit source of truth

---

## Security Considerations

1. **SQL Injection Prevention** - Use parameterized queries
2. **Balance Validation** - CHECK constraints prevent negative balances
3. **Immutable Audit Trail** - Ledger cannot be tampered
4. **Idempotency** - Prevents duplicate charges
5. **Request Validation** - Hash prevents key reuse with different payloads

---

## Summary

This schema design provides:
- ✅ **Atomic idempotency** via database UNIQUE constraint
- ✅ **Race condition prevention** via INSERT ON CONFLICT
- ✅ **Request validation** via hash comparison
- ✅ **Concurrency safety** via deterministic locking
- ✅ **Audit trail** via immutable ledger
- ✅ **Data integrity** via database constraints
- ✅ **Performance** via stored balances and indexes
- ✅ **Production-grade** patterns used by Stripe, Square, etc.

The design prioritizes **correctness** and **safety** for financial transactions while maintaining good performance.
