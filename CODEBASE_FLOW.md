# Wallet Transfer Service - Complete Flow & Architecture

## Overview

This is a wallet-to-wallet transfer service built with Go, GORM, and PostgreSQL. It implements a clean layered architecture with proper transaction handling, idempotency guarantees, and double-entry ledger accounting.

---

## System Architecture

### Layer Stack (Top to Bottom)

```
┌─────────────────────────────────────┐
│        HTTP Handlers Layer          │
│    (Transfer Request/Response)      │
└─────────────────┬───────────────────┘
                  │
┌─────────────────▼───────────────────┐
│       Service Layer (Business Logic)│
│   (ExecuteTransfer Orchestration)   │
└─────────────────┬───────────────────┘
                  │
┌─────────────────▼───────────────────┐
│    Repository Layer (Persistence)   │
│  (GORM Models & Database Operations)│
└─────────────────┬───────────────────┘
                  │
┌─────────────────▼───────────────────┐
│      Domain Models Layer            │
│  (Wallet, Transfer, LedgerEntry,    │
│   IdempotencyRecord)                │
└─────────────────┬───────────────────┘
                  │
┌─────────────────▼───────────────────┐
│  PostgreSQL Database + Indexes      │
│  (Tables: wallets, transfers,       │
│   ledger_entries, idempotency_recs) │
└─────────────────────────────────────┘
```

---

## Request Flow

### 1. HTTP Request Enters Handler Layer

```
POST /transfers
Content-Type: application/json

{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

### 2. Handler (CreateTransfer)

**File:** `internal/handlers/transfer.go`

```
Handler receives request
    ↓
Parse JSON request body
    ↓
Validate required fields (idempotencyKey, fromWalletId, toWalletId, amount)
    ↓
Call service.ExecuteTransfer(ctx, idempotencyKey, fromWalletId, toWalletId, amount)
    ↓
If error: return HTTP error response (400, 422, 500)
If success: return 200 OK with TransferResponse
```

**Responsibilities:**

- Parse HTTP request
- Validate required fields are present
- Map HTTP status codes to errors
- Serialize response to JSON

---

### 3. Service Layer (ExecuteTransfer)

**File:** `internal/services/transfer.go`

#### Step 1: Validate Transfer Input

```
Transfer.Validate()
    ├─ amount > 0?
    ├─ fromWalletId ≠ toWalletId?
    └─ both wallet IDs provided?
```

#### Step 2: Check Idempotency

```
idempotencyRepo.GetByKey(idempotencyKey)
    ├─ If found: return cached TransferResponse (idempotent ✓)
    └─ If not found: proceed to step 3
```

#### Step 3: Begin Transaction

```
tx := db.BeginTx(ctx, nil)
    └─ All subsequent operations use this transaction
    └─ If error: rollback, return error
```

#### Step 4: Create Transfer Record (PENDING)

```
transferRepo.Create(tx, fromWalletId, toWalletId, amount, transferID)
    ├─ Generate UUID for transferID
    ├─ Create Transfer with status=PENDING
    └─ Store in database
```

#### Step 5: Lock from_wallet (Row-Level Lock)

```
walletRepo.GetForUpdate(tx, fromWalletId)
    ├─ SELECT ... FOR UPDATE (locks row)
    ├─ If not found: tx.Rollback(), return "wallet not found"
    └─ If found: check balance
```

#### Step 6: Verify Sufficient Balance

```
if fromWallet.Balance < amount:
    ├─ tx.Rollback()
    └─ return "insufficient balance: have X, need Y"
```

#### Step 7: Lock to_wallet (Row-Level Lock)

```
walletRepo.GetForUpdate(tx, toWalletId)
    ├─ SELECT ... FOR UPDATE (locks row)
    └─ If not found: tx.Rollback(), return "wallet not found"
```

#### Step 8: Create DEBIT Ledger Entry

```
ledgerRepo.Create(tx, fromWalletId, transferId, DEBIT, amount)
    ├─ Generate UUID for entryID
    ├─ Insert: entry_id, wallet_id, transfer_id, type=DEBIT, amount
    └─ This records outgoing money from source wallet
```

#### Step 9: Create CREDIT Ledger Entry

```
ledgerRepo.Create(tx, toWalletId, transferId, CREDIT, amount)
    ├─ Generate UUID for entryID
    ├─ Insert: entry_id, wallet_id, transfer_id, type=CREDIT, amount
    └─ This records incoming money to destination wallet
```

#### Step 10: Update from_wallet Balance (Debit)

```
walletRepo.UpdateBalance(tx, fromWalletId, -amount)
    └─ UPDATE wallets SET balance = balance - amount WHERE wallet_id = fromWalletId
```

#### Step 11: Update to_wallet Balance (Credit)

```
walletRepo.UpdateBalance(tx, toWalletId, +amount)
    └─ UPDATE wallets SET balance = balance + amount WHERE wallet_id = toWalletId
```

#### Step 12: Update Transfer Status to PROCESSED

```
transferRepo.UpdateStatus(tx, transferId, PROCESSED)
    └─ UPDATE transfers SET status = PROCESSED WHERE transfer_id = transferId
```

#### Step 13: Record Idempotency

```
idempotencyRepo.Record(tx, idempotencyKey, transferId, response)
    ├─ Serialize response to JSON
    ├─ Insert: idempotency_key, transfer_id, response (JSONB)
    └─ UNIQUE constraint on idempotency_key ensures no duplicates
```

#### Step 14: Commit Transaction

```
tx.Commit()
    ├─ If error: return error
    └─ If success: return TransferResponse
```

#### Step 15: Return Response

```
TransferResponse {
    transfer_id: "uuid",
    status: "PROCESSED",
    from_wallet_id: "wallet_1",
    to_wallet_id: "wallet_2",
    amount: 100
}
```

---

## Complete Data Flow Diagram

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  CLIENT                                                                      │
│  POST /transfers                                                             │
│  {idempotencyKey, fromWalletId, toWalletId, amount}                          │
└────────────────────────────────┬─────────────────────────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  HANDLER LAYER (handlers/transfer.go)                                        │
│  ├─ Parse JSON                                                               │
│  ├─ Validate required fields                                                 │
│  └─ Call service.ExecuteTransfer()                                           │
└────────────────────────────────┬─────────────────────────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  SERVICE LAYER (services/transfer.go)                                        │
│  ├─ Validate transfer (amount > 0, wallets differ)                          │
│  ├─ Check idempotency                                                        │
│  │  └─ If found: return cached response (IDEMPOTENT) ✓                       │
│  ├─ Begin transaction                                                        │
│  └─ Orchestrate transfer execution:                                         │
│     ├─ Create transfer (PENDING)                                             │
│     ├─ Lock from_wallet (FOR UPDATE)                                         │
│     ├─ Verify balance                                                        │
│     ├─ Lock to_wallet (FOR UPDATE)                                           │
│     ├─ Create DEBIT ledger entry                                             │
│     ├─ Create CREDIT ledger entry                                            │
│     ├─ Update from_wallet balance (debit)                                    │
│     ├─ Update to_wallet balance (credit)                                     │
│     ├─ Update transfer status → PROCESSED                                    │
│     ├─ Record idempotency                                                    │
│     └─ Commit transaction                                                    │
└────────────────────────────────┬─────────────────────────────────────────────┘
                                 │
                ┌────────────────┼────────────────┐
                │                │                │
                ▼                ▼                ▼
    ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
    │  REPOSITORIES    │ │  REPOSITORIES    │ │  REPOSITORIES    │
    │  (wallet.go)     │ │  (transfer.go)   │ │  (ledger.go)     │
    │                  │ │                  │ │                  │
    │  GetForUpdate()  │ │  Create()        │ │  Create()        │
    │  UpdateBalance() │ │  UpdateStatus()  │ │  VerifyBalance() │
    │  GetBalance()    │ │                  │ │                  │
    │  GetByID()       │ │                  │ │                  │
    │  Create()        │ │                  │ │                  │
    └──────────────────┘ └──────────────────┘ └──────────────────┘
                │                │                │
                └────────────────┼────────────────┘
                                 │
                ┌────────────────┴────────────────┐
                │                                 │
                ▼                                 ▼
    ┌──────────────────┐             ┌──────────────────┐
    │  IDEMPOTENCY     │             │  MODELS          │
    │  REPOSITORY      │             │  (models/)       │
    │  (idempotency.go)│             │                  │
    │                  │             │  • Transfer      │
    │  GetByKey()      │             │  • Wallet        │
    │  Record()        │             │  • LedgerEntry   │
    └────────┬─────────┘             │  • Idempotency   │
             │                       │    Record        │
             └───────────┬───────────┘                  │
                         │                             │
                         ▼                             │
        ┌────────────────────────────────────────────┐ │
        │  GORM + PostgreSQL Database                │ │
        │                                            │ │
        │  Tables:                                   │ │
        │  ├─ wallets (wallet_id PK, balance)       │ │
        │  ├─ transfers (transfer_id PK, status)    │ │
        │  ├─ ledger_entries (entry_id PK)          │ │
        │  │  └─ Stores DEBIT & CREDIT entries      │ │
        │  └─ idempotency_records                   │ │
        │     └─ idempotency_key UNIQUE PRIMARY KEY │ │
        │     └─ Caches transfer response           │ │
        │                                            │ │
        │  Constraints & Indexes:                   │ │
        │  ├─ UNIQUE idempotency_key                │ │
        │  ├─ Foreign keys between tables           │ │
        │  ├─ CHECK constraint: type IN (DEBIT,     │ │
        │  │  CREDIT)                               │ │
        │  └─ Indexes on frequently queried columns │ │
        │                                            │ │
        │  Transaction Isolation: Read Committed    │ │
        │  Row-Level Locks: SELECT...FOR UPDATE     │ │
        └────────────────────────────────────────────┘ │
                                                        │
                                                        ▼
                                            ┌──────────────────────┐
                                            │  VALIDATE & RETURN   │
                                            │  • Ledger balanced?  │
                                            │  • Balances match?   │
                                            │  • 2 entries per txn?│
                                            └──────────────────────┘
```

---

## Concurrency & Safety Mechanisms

### 1. Row-Level Locking

```
When transfer from wallet_A to wallet_B:

Transaction 1                          Transaction 2
─────────────────────────────────      ─────────────────────────────────
BEGIN                                  BEGIN
  SELECT ... FOR UPDATE wallet_A       (waits for lock)
  LOCK ACQUIRED ✓

  SELECT ... FOR UPDATE wallet_B       SELECT ... FOR UPDATE wallet_A
  LOCK ACQUIRED ✓                      (waits for lock)

  UPDATE wallet_A balance
  UPDATE wallet_B balance

COMMIT                                 (now lock released, proceeds)
```

### 2. Idempotency Guarantees

```
First Request:                         Duplicate Request (same key):
─────────────────────────────────      ─────────────────────────────────
Check idempotency_records
  NOT FOUND                            Check idempotency_records
                                         FOUND ✓
Execute transfer
  Create transfer                      Return cached response
  Create ledger entries                  (no duplicate transfer)
  Update balances
  Record idempotency

Return response                        Return response
```

### 3. Transaction Atomicity

```
All-or-Nothing Guarantee:

BEGIN TRANSACTION
  ├─ Create transfer (PENDING)
  ├─ Lock wallets
  ├─ Create ledger entries
  ├─ Update balances
  ├─ Update transfer (PROCESSED)
  ├─ Record idempotency
  │
  ├─ On ERROR anywhere: ROLLBACK
  │  └─ All changes reversed ✓
  │
  └─ On SUCCESS everywhere: COMMIT
     └─ All changes persisted ✓
```

### 4. Double-Entry Ledger Consistency

```
Every Transfer = Always 2 entries:

Transfer from W1 to W2, amount 100:

Ledger Entry 1 (DEBIT):
  wallet_id: W1
  type: DEBIT
  amount: 100

Ledger Entry 2 (CREDIT):
  wallet_id: W2
  type: CREDIT
  amount: 100

Invariant: SUM(DEBIT) = SUM(CREDIT) ✓ (balance check)
```

---

## Error Handling Flow

```
ExecuteTransfer
  │
  ├─ Validation Error (amount ≤ 0, same wallet)
  │  └─ Return: 400 Bad Request
  │
  ├─ Idempotency Check Error
  │  └─ Return: 500 Internal Server Error
  │
  ├─ Transaction Begin Error
  │  └─ Return: 500 Internal Server Error
  │
  ├─ Wallet Not Found
  │  ├─ Rollback transaction
  │  └─ Return: 422 Unprocessable Entity
  │
  ├─ Insufficient Balance
  │  ├─ Rollback transaction
  │  └─ Return: 422 Unprocessable Entity
  │
  ├─ Database Operation Error (create, update)
  │  ├─ Rollback transaction
  │  └─ Return: 500 Internal Server Error
  │
  ├─ Commit Error
  │  └─ Return: 500 Internal Server Error
  │
  └─ Success
     └─ Return: 200 OK with TransferResponse
```

---

## Data Models

### Wallet

```go
type Wallet struct {
    WalletID  string    `gorm:"primaryKey"`
    Balance   int64     // Current balance
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Transfer

```go
type Transfer struct {
    TransferID   string    `gorm:"primaryKey"`
    FromWalletID string    `gorm:"index"`
    ToWalletID   string    `gorm:"index"`
    Amount       int64
    Status       string    // PENDING, PROCESSED, FAILED
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

// States: PENDING → (PROCESSED | FAILED)
```

### LedgerEntry (Double-Entry)

```go
type LedgerEntry struct {
    EntryID    string    `gorm:"primaryKey"`
    WalletID   string    `gorm:"index"`
    TransferID string    `gorm:"index"`
    Type       string    // DEBIT or CREDIT
    Amount     int64
    CreatedAt  time.Time
}

// Per transfer: exactly 1 DEBIT + 1 CREDIT
```

### IdempotencyRecord

```go
type IdempotencyRecord struct {
    IdempotencyKey string `gorm:"primaryKey;unique"` // Unique constraint
    TransferID     string
    Response       []byte // JSON-serialized TransferResponse
    CreatedAt      time.Time
}
```

---

## Database Schema

```sql
CREATE TABLE wallets (
    wallet_id UUID PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE transfers (
    transfer_id UUID PRIMARY KEY,
    from_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    amount BIGINT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE TABLE ledger_entries (
    entry_id UUID PRIMARY KEY,
    wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id),
    type VARCHAR(10) NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY UNIQUE,
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id),
    response JSONB NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for query performance
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_ledger_wallet_transfer ON ledger_entries(wallet_id, transfer_id);
```

---

## Key Design Decisions

| Decision                         | Reason                                                |
| -------------------------------- | ----------------------------------------------------- |
| **GORM ORM**                     | Type-safe queries, automatic migrations, cleaner code |
| **Row-Level Locks (FOR UPDATE)** | Prevents concurrent double-spending on same wallet    |
| **Double-Entry Ledger**          | Ensures financial correctness (DEBIT = CREDIT)        |
| **Idempotency Records Table**    | UNIQUE constraint provides deduplication guarantee    |
| **Transaction Wrapping**         | Atomic all-or-nothing semantics                       |
| **Balance + Ledger**             | Balance for O(1) reads, ledger for audit trail        |
| **UUID for IDs**                 | Distributed-friendly, collision-resistant             |
| **JSON Response Caching**        | Fast idempotent response without recalculation        |

---

## Testing Strategy

### Unit Tests

- Transfer validation (amount, wallets, same wallet check)
- State transitions (PENDING → PROCESSED/FAILED)
- Ledger balance verification

### Integration Tests

- **Happy Path**: transfer succeeds, balances correct
- **Idempotency**: duplicate request returns same result
- **Concurrency**: 2+ transfers from same wallet (no negative balance)
- **Failure Scenarios**: insufficient balance, invalid wallet, rollback
- **Ledger Consistency**: SUM(DEBIT) = SUM(CREDIT)

---

## Performance Characteristics

| Operation             | Complexity | Time                      |
| --------------------- | ---------- | ------------------------- |
| Check balance         | O(1)       | ~1ms                      |
| Execute transfer      | O(n)       | ~10-50ms (with lock wait) |
| Check idempotency     | O(1)       | ~1ms                      |
| Get transfer history  | O(n log n) | ~5-20ms                   |
| Verify ledger balance | O(n)       | ~2-10ms                   |

---

## Deployment Notes

1. **Database Setup**
   - Create database: `createdb -U admin my_db`
   - Run migrations via GORM AutoMigrate or SQL script

2. **Environment**

   ```bash
   DB_HOST=localhost
   DB_PORT=5432
   DB_USER=admin
   DB_PASSWORD=mydb
   DB_NAME=my_db
   PORT=8080
   ```

3. **Connection Pooling**
   - GORM default: 10 connections
   - Tunable via `db.DB().SetMaxOpenConns(25)`

4. **Monitoring**
   - Log all transfers (with IDs)
   - Monitor idempotency hit rate
   - Track lock contention on wallet rows
   - Health check: `GET /health` → 200 OK
