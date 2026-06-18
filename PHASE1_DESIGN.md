# Phase 1: Design & Documentation

## Overview

This document outlines the design decisions for the Wallet Transfer Service, a Spring Boot application implementing reliable wallet-to-wallet transfers with idempotency, double-entry ledger, and concurrency safety.

---

## 1. Database Schema Design

### 1.1 Wallets Table

```sql
CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    wallet_id VARCHAR(255) UNIQUE NOT NULL,
    balance DECIMAL(19, 2) NOT NULL DEFAULT 0.00,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
)
```

**Design Rationale:**
- `wallet_id`: Business key (unique identifier provided by API clients)
- `id`: Technical surrogate key (UUID for internal references)
- `balance`: Maintained for query efficiency (derived from ledger but cached)
- CHECK constraint ensures balance >= 0 (prevents negative balances)
- `updated_at` tracks last modification for optimistic locking

### 1.2 Transfers Table

```sql
CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    transfer_id VARCHAR(255) UNIQUE NOT NULL,
    from_wallet_id VARCHAR(255) NOT NULL,
    to_wallet_id VARCHAR(255) NOT NULL,
    amount DECIMAL(19, 2) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (from_wallet_id) REFERENCES wallets(wallet_id),
    FOREIGN KEY (to_wallet_id) REFERENCES wallets(wallet_id)
)
```

**Design Rationale:**
- `transfer_id`: Business key (unique transfer identifier)
- `idempotency_key`: UNIQUE constraint enables deduplication (idempotency guarantee)
- `status`: Tracks transfer state (PENDING → PROCESSED/FAILED)
- CHECKs prevent invalid transfers (amount > 0, from != to)
- Foreign keys ensure referential integrity

### 1.3 Ledger Entries Table

```sql
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    entry_id VARCHAR(255) UNIQUE NOT NULL,
    transfer_id VARCHAR(255) NOT NULL,
    wallet_id VARCHAR(255) NOT NULL,
    entry_type VARCHAR(32) NOT NULL,  -- DEBIT or CREDIT
    amount DECIMAL(19, 2) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (transfer_id) REFERENCES transfers(transfer_id),
    FOREIGN KEY (wallet_id) REFERENCES wallets(wallet_id)
)
```

**Design Rationale:**
- Double-entry bookkeeping: Every transfer creates 2 entries (DEBIT + CREDIT)
- `entry_type` (DEBIT/CREDIT) for audit and reconciliation
- Entries are immutable (no updates, only inserts)
- Unique `entry_id` per ledger entry
- Foreign key cascade ensures transfer deletion cleans up ledger

### 1.4 Idempotency Records Table

```sql
CREATE TABLE idempotency_records (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    transfer_id VARCHAR(255) NOT NULL,
    response_status_code INT NOT NULL,
    response_body TEXT,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (transfer_id) REFERENCES transfers(transfer_id)
)
```

**Design Rationale:**
- Stores the original response for repeated requests
- UNIQUE constraint on `idempotency_key` prevents duplicates at DB level
- Enables "exactly-once" semantics: return cached result on duplicate

---

## 2. API Contract

### Create Transfer Endpoint

**Request:**
```http
POST /api/transfers
Content-Type: application/json

{
  "idempotencyKey": "unique-key-123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.00
}
```

**Success Response (201 Created):**
```json
{
  "transferId": "txn_abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.00,
  "status": "PROCESSED",
  "createdAt": "2026-06-16T10:30:00Z"
}
```

**Duplicate Request Response (200 OK):**
```json
{
  "transferId": "txn_abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.00,
  "status": "PROCESSED",
  "createdAt": "2026-06-16T10:30:00Z"
}
```

**Error Responses:**
- `400 Bad Request`: Invalid request (missing fields, negative amount, from==to)
- `422 Unprocessable Entity`: Business logic error (insufficient balance)
- `500 Internal Server Error`: Database or system error

---

## 3. Idempotency Strategy

### 3.1 Mechanism

**Unique Constraint Approach:**
- `UNIQUE(idempotency_key)` on transfers table
- First request creates transfer record
- Duplicate request fails constraint → caught and returns original result

### 3.2 Implementation Flow

```
1. Client sends request with idempotencyKey
2. Check idempotency_records table for key
   ├─ If found → return cached response (HTTP 200)
   └─ If not found → proceed to step 3
3. Attempt to INSERT into transfers table with idempotencyKey
   ├─ Success → execute transfer logic (step 4)
   └─ Conflict (UNIQUE) → another request won (handled by DB)
4. Execute transfer atomically:
   - Debit source wallet
   - Credit destination wallet
   - Insert ledger entries (2 entries)
   - Update transfer status to PROCESSED
5. INSERT into idempotency_records with response
6. Return response (HTTP 201)
```

### 3.3 Guarantees

- **Exactly-Once Semantics**: idempotencyKey UNIQUE constraint ensures only 1 transfer per key
- **No Duplicate Side Effects**: Even if request retries, ledger has only 1 transfer worth of entries
- **Safe Under Network Failures**: Timeout → retry with same key → returns original result

---

## 4. Concurrency & Transaction Safety

### 4.1 Challenge: Concurrent Debits on Same Wallet

**Scenario:**
```
T1: Debit $100 from wallet_A
T2: Debit $200 from wallet_A (simultaneously)
wallet_A has only $150
```

**Expected Behavior:** One succeeds, one fails with "insufficient balance"

### 4.2 Concurrency Strategy: SERIALIZABLE Isolation

**Transaction Isolation Level:**
```
SET TRANSACTION ISOLATION LEVEL SERIALIZABLE
```

**Rationale:**
- Prevents dirty reads, non-repeatable reads, and phantom reads
- Serializes conflicting transactions (one after the other)
- For wallet transfers, correctness > performance
- Locks acquired automatically on table/row reads

**Flow:**
```
T1: BEGIN (SERIALIZABLE)
    SELECT balance FROM wallets WHERE wallet_id = 'wallet_A' FOR UPDATE
    → Lock acquired, balance = $150
    → Enough for $100 debit? YES
    → UPDATE wallets SET balance = $50
    COMMIT
    
T2 (waiting on T1's lock): BEGIN (SERIALIZABLE)
    SELECT balance FROM wallets WHERE wallet_id = 'wallet_A' FOR UPDATE
    → Acquires lock after T1 commits
    → balance = $50
    → Enough for $200 debit? NO
    → ROLLBACK (throw exception)
```

### 4.3 Row-Level Lock Strategy

**Implementation:**
- Use `@Lock(LockModeType.PESSIMISTIC_WRITE)` in Spring Data JPA
- `SELECT ... FOR UPDATE` on wallet balance check
- Locks released after transaction commits

**Benefits:**
- Prevents race conditions on wallet balance
- Clear ordering of operations
- Predictable and debuggable

### 4.4 Transaction Boundaries

**Atomic Transfer Transaction:**

```java
@Transactional(isolation = Isolation.SERIALIZABLE)
public Transfer executeTransfer(String idempotencyKey, ...) {
    // 1. Lock source wallet, check balance
    Wallet source = walletRepository.findByIdForUpdate(fromWalletId);
    
    // 2. Lock destination wallet
    Wallet dest = walletRepository.findByIdForUpdate(toWalletId);
    
    // 3. Validate and debit
    if (source.getBalance() < amount) throw new InsufficientBalanceException();
    source.setBalance(source.getBalance() - amount);
    
    // 4. Credit destination
    dest.setBalance(dest.getBalance() + amount);
    
    // 5. Create transfer record
    Transfer transfer = new Transfer(...);
    transfer.setStatus(TransferStatus.PROCESSED);
    transferRepository.save(transfer);
    
    // 6. Create ledger entries (2 entries per transfer)
    ledgerRepository.save(new LedgerEntry(transfer.getId(), fromWalletId, DEBIT, amount));
    ledgerRepository.save(new LedgerEntry(transfer.getId(), toWalletId, CREDIT, amount));
    
    // 7. Save idempotency record
    idempotencyRepository.save(new IdempotencyRecord(idempotencyKey, transfer.getId(), ...));
    
    return transfer;
}
// COMMIT or ROLLBACK (all-or-nothing)
```

**All-or-Nothing Guarantee:**
- Single `@Transactional` method
- Any failure → entire transaction rolls back
- Database handles commit/rollback

---

## 5. Transfer State Machine

### 5.1 States

- **PENDING**: Initial state, transfer created but not processed
- **PROCESSED**: Successfully executed (balances updated, ledger entries created)
- **FAILED**: Execution failed (insufficient balance, internal error)

### 5.2 Valid Transitions

```
PENDING → PROCESSED (success)
PENDING → FAILED (error)
```

**Transitions are unidirectional and final** (no retries change state, retries return cached result).

---

## 6. Ledger Consistency Rules

### 6.1 Double-Entry Invariant

**For every transfer:**
```
∑ DEBIT entries = ∑ CREDIT entries = transfer.amount
```

**Per Transfer Example:**
```
Transfer T1: $100 from wallet_A to wallet_B

Ledger Entry 1:
  transfer_id = T1
  wallet_id = wallet_A
  entry_type = DEBIT
  amount = 100

Ledger Entry 2:
  transfer_id = T1
  wallet_id = wallet_B
  entry_type = CREDIT
  amount = 100
```

### 6.2 Balance Reconciliation

**Derived Balance (from ledger):**
```sql
SELECT wallet_id, SUM(CASE WHEN entry_type='CREDIT' THEN amount ELSE -amount END) as balance
FROM ledger_entries
GROUP BY wallet_id
```

**Stored Balance (in wallets table):**
```sql
SELECT wallet_id, balance FROM wallets
```

**Guarantee:** Stored balance always matches derived balance (maintained during transactions).

---

## 7. Error Handling & Recovery

### 7.1 Failure Scenarios

| Scenario | Cause | Recovery |
|----------|-------|----------|
| Insufficient Balance | Account has < transfer amount | Fail transfer, status=FAILED |
| Wallet Not Found | fromWalletId or toWalletId invalid | 400 Bad Request |
| Duplicate Idempotency Key | Retry with same key | Return cached response |
| DB Constraint Violation | Data integrity error | 500 Internal Server Error |
| Network Timeout | Connection lost mid-transaction | Retry with same idempotencyKey |
| Concurrent Lock Timeout | Deadlock or lock wait timeout | Retry (application level or DB auto-retry) |

### 7.2 Idempotent Retry Behavior

**Client Retries:**
```
Attempt 1: POST /transfers + idempotencyKey → Success (201)
Attempt 2: POST /transfers + idempotencyKey → Cached (200) [idempotent]
Attempt 3: POST /transfers + idempotencyKey → Cached (200) [idempotent]
```

---

## 8. Architecture Layers

```
┌─────────────────────────────────────┐
│         HTTP Handler Layer          │
│  (WalletTransferController)         │
│  - Request validation               │
│  - Transport mapping                │
│  - Status code mapping              │
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│         Service Layer               │
│  (WalletTransferService)            │
│  - Business logic                   │
│  - Idempotency check                │
│  - Transaction orchestration        │
│  - Concurrency handling             │
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│      Repository Layer               │
│  (WalletRepository, etc.)           │
│  - Database access                  │
│  - Query construction               │
│  - Entity mapping                   │
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│      Domain Model Layer             │
│  (Wallet, Transfer, LedgerEntry)    │
│  - JPA entities                     │
│  - Validation rules                 │
│  - State management                 │
└─────────────────────────────────────┘
```

---

## 9. Testing Strategy

### 9.1 Unit Tests (Service Layer)
- Transfer execution logic
- Balance calculation
- State transitions
- Idempotency behavior

### 9.2 Integration Tests (DB + Service)
- End-to-end transfer with ledger verification
- Concurrent transfers on same wallet
- Idempotency deduplication
- Insufficient balance scenarios

### 9.3 Concurrency Tests
- Simulate 10+ concurrent transfers on same wallet
- Verify no race conditions
- Verify ledger consistency

---

## 10. Summary

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| Database | PostgreSQL | Reliable ACID transactions, JSON support, performance |
| Isolation Level | SERIALIZABLE | Concurrency safety over throughput |
| Idempotency | UNIQUE constraint | DB-enforced, simplest guarantee |
| Balance Strategy | Stored + Derived | Efficiency (stored) + auditability (derived) |
| Ledger | Double-entry | Financial correctness, audit trail |
| Transaction Scope | Single method | Atomic all-or-nothing |
| Locking | Pessimistic (FOR UPDATE) | Predictable, prevents race conditions |

---

**Next Phase:** Implement domain models and repository layer (Phase 2)
