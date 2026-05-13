# Wallet Transfer Service - Visual Flow Diagrams

## 1. High-Level Request Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                 │
│                    CLIENT REQUEST                              │
│            POST /transfers (with idempotencyKey)               │
│                                                                 │
└────────────────────────────┬──────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│  HANDLER LAYER                                                  │
│  ├─ Parse JSON                                                  │
│  ├─ Validate required fields                                    │
│  └─ Delegate to service                                         │
└────────────────────────────┬──────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│  SERVICE LAYER - ExecuteTransfer()                              │
│  ├─ Validate transfer (amount > 0, wallets differ)             │
│  ├─ Check idempotency key                                       │
│  │  │                                                           │
│  │  ├─ If exists: RETURN cached response ✓ (FAST)             │
│  │  │                                                           │
│  │  └─ If not: Begin transaction                               │
│  │      ├─ Create transfer (PENDING)                            │
│  │      ├─ Lock source wallet (FOR UPDATE)                      │
│  │      ├─ Verify balance                                       │
│  │      ├─ Lock destination wallet (FOR UPDATE)                 │
│  │      ├─ Create DEBIT ledger entry                            │
│  │      ├─ Create CREDIT ledger entry                           │
│  │      ├─ Update both balances (atomic)                        │
│  │      ├─ Update transfer to PROCESSED                         │
│  │      ├─ Record idempotency key                               │
│  │      └─ Commit transaction                                   │
│  │                                                              │
│  └─ Return response                                             │
└────────────────────────────┬──────────────────────────────────┘
                             │
                    ┌────────┴────────┐
                    │                 │
                    ▼                 ▼
         ┌────────────────────┐ ┌──────────────────┐
         │  Wallet            │ │  Transfer        │
         │  Repository        │ │  Repository      │
         │  ├─ GetForUpdate() │ │  ├─ Create()     │
         │  ├─ UpdateBalance()│ │  └─ UpdateStatus()
         │  └─ GetBalance()   │ │                  │
         └────────────────────┘ └──────────────────┘
                    │                 │
                    ▼                 ▼
         ┌────────────────────┐ ┌──────────────────┐
         │  Ledger            │ │  Idempotency     │
         │  Repository        │ │  Repository      │
         │  └─ Create()       │ │  └─ Record()     │
         └────────────────────┘ └──────────────────┘
                    │
                    ▼
         ┌────────────────────────────────────┐
         │  PostgreSQL Database               │
         │  (tables, locks, constraints)      │
         └────────────────────────────────────┘
                    │
                    ▼
         ┌────────────────────────────────────┐
         │  Response to Client                │
         │  (transfer_id, status, amount)     │
         └────────────────────────────────────┘
```

---

## 2. Transaction Lifecycle Diagram

```
START: HTTP POST /transfers request arrives
│
├─ [1] VALIDATE INPUT
│   ├─ Amount > 0?
│   ├─ fromWalletId ≠ toWalletId?
│   └─ Required fields present?
│   │
│   └─ ❌ FAIL → HTTP 400 Bad Request
│
├─ [2] CHECK IDEMPOTENCY
│   ├─ Query: idempotency_records WHERE idempotency_key = ?
│   ├─ ✓ FOUND → Return cached response (HTTP 200)
│   └─ NOT FOUND → Proceed to step 3
│
├─ [3] BEGIN TRANSACTION
│   └─ START TRANSACTION in PostgreSQL
│
├─ [4] CREATE TRANSFER RECORD (PENDING)
│   ├─ INSERT INTO transfers (transfer_id, from_wallet_id, to_wallet_id, amount, status)
│   │           VALUES (UUID(), ?, ?, ?, 'PENDING')
│   └─ ❌ ERROR → ROLLBACK, HTTP 500
│
├─ [5] LOCK SOURCE WALLET
│   ├─ SELECT * FROM wallets WHERE wallet_id = ? FOR UPDATE
│   ├─ Row locked exclusively
│   └─ ❌ NOT FOUND → ROLLBACK, HTTP 422 (wallet not found)
│
├─ [6] VERIFY BALANCE
│   ├─ Check: fromWallet.balance >= amount
│   └─ ❌ INSUFFICIENT → ROLLBACK, HTTP 422
│
├─ [7] LOCK DESTINATION WALLET
│   ├─ SELECT * FROM wallets WHERE wallet_id = ? FOR UPDATE
│   └─ ❌ NOT FOUND → ROLLBACK, HTTP 422
│
├─ [8] CREATE DEBIT LEDGER ENTRY
│   ├─ INSERT INTO ledger_entries
│   │        (entry_id, wallet_id, transfer_id, type, amount)
│   │  VALUES (UUID(), fromWalletId, transferId, 'DEBIT', amount)
│   └─ ❌ ERROR → ROLLBACK, HTTP 500
│
├─ [9] CREATE CREDIT LEDGER ENTRY
│   ├─ INSERT INTO ledger_entries
│   │        (entry_id, wallet_id, transfer_id, type, amount)
│   │  VALUES (UUID(), toWalletId, transferId, 'CREDIT', amount)
│   └─ ❌ ERROR → ROLLBACK, HTTP 500
│
├─ [10] UPDATE SOURCE WALLET BALANCE
│   ├─ UPDATE wallets SET balance = balance - amount
│   │   WHERE wallet_id = fromWalletId
│   └─ ❌ ERROR → ROLLBACK, HTTP 500
│
├─ [11] UPDATE DESTINATION WALLET BALANCE
│   ├─ UPDATE wallets SET balance = balance + amount
│   │   WHERE wallet_id = toWalletId
│   └─ ❌ ERROR → ROLLBACK, HTTP 500
│
├─ [12] UPDATE TRANSFER STATUS TO PROCESSED
│   ├─ UPDATE transfers SET status = 'PROCESSED'
│   │   WHERE transfer_id = transferId
│   └─ ❌ ERROR → ROLLBACK, HTTP 500
│
├─ [13] RECORD IDEMPOTENCY
│   ├─ INSERT INTO idempotency_records
│   │        (idempotency_key, transfer_id, response)
│   │  VALUES (idempotencyKey, transferId, responseJSON)
│   ├─ UNIQUE constraint on idempotency_key
│   └─ ❌ ERROR → ROLLBACK, HTTP 500
│
├─ [14] COMMIT TRANSACTION
│   ├─ COMMIT (all changes persisted atomically)
│   └─ ❌ ERROR → HTTP 500
│
└─ [15] RETURN RESPONSE (HTTP 200)
    ├─ transfer_id: UUID
    ├─ status: "PROCESSED"
    ├─ from_wallet_id: fromWalletId
    ├─ to_wallet_id: toWalletId
    ├─ amount: amount
    └─ timestamp: ISO 8601
```

---

## 3. Concurrency Safety - Lock Sequence Diagram

```
Transaction A (Transfer W1 → W2)    Transaction B (Transfer W1 → W3)
═════════════════════════════════    ═════════════════════════════════

BEGIN                                BEGIN

CREATE transfer A                    CREATE transfer B

SELECT * FROM wallets               SELECT * FROM wallets
WHERE wallet_id = W1                WHERE wallet_id = W1
FOR UPDATE                          FOR UPDATE (WAITS)
[LOCK W1 ACQUIRED] ✓               [LOCKED - BLOCKED]

Check balance
(W1 balance = 1000)
Balance >= 100? YES ✓

SELECT * FROM wallets
WHERE wallet_id = W2
FOR UPDATE                          [STILL WAITING...]
[LOCK W2 ACQUIRED] ✓

CREATE ledger entries
(DEBIT W1, CREDIT W2)

UPDATE W1 balance                   [STILL WAITING...]
(1000 - 100 = 900)

UPDATE W2 balance                   [STILL WAITING...]
(2000 + 100 = 2100)

UPDATE transfer A.status
= PROCESSED

COMMIT                              [LOCK W1 RELEASED]

[LOCK W2 RELEASED]                  SELECT * FROM wallets
[LOCK W1 RELEASED]                  WHERE wallet_id = W1
                                    FOR UPDATE
                                    [LOCK ACQUIRED] ✓

                                    Check balance
                                    (W1 balance = 900)  ← Updated by A!
                                    Balance >= 50? YES ✓

                                    [Transaction B proceeds...]
                                    [Eventually COMMITS]
```

**Result**: No race condition! Transaction B sees updated balance from A.

---

## 4. Idempotency - Request Retry Flow

```
FIRST REQUEST
═════════════════════════════════════════

POST /transfers
  idempotencyKey: "ABC123"
  fromWalletId: "W1"
  toWalletId: "W2"
  amount: 100

│
├─ Check idempotency_records
│  └─ NOT FOUND
│
├─ Execute transfer
│  ├─ Create transfer (PENDING)
│  ├─ Lock wallets
│  ├─ Create ledger entries
│  ├─ Update balances
│  ├─ Update transfer → PROCESSED
│  └─ Record idempotency
│
└─ Response: HTTP 200
   {
     "transfer_id": "TXN-UUID-123",
     "status": "PROCESSED",
     "amount": 100
   }


RETRY REQUEST (network timeout caused retry)
═════════════════════════════════════════════

POST /transfers
  idempotencyKey: "ABC123"  ← SAME KEY
  fromWalletId: "W1"
  toWalletId: "W2"
  amount: 100

│
├─ Check idempotency_records
│  └─ FOUND!
│
├─ Retrieve cached response
│  └─ Deserialize idempotency_records.response
│
└─ Response: HTTP 200 (IMMEDIATE - no DB write!)
   {
     "transfer_id": "TXN-UUID-123",  ← SAME ID
     "status": "PROCESSED",
     "amount": 100
   }


RESULT
══════
✓ Only ONE transfer created (TXN-UUID-123)
✓ Balances updated only ONCE
✓ Retry gets immediate response
✓ Exactly-once semantics guaranteed
```

---

## 5. Double-Entry Ledger - Balance Verification

```
Transfer: W1 → W2, amount = 100

STATE BEFORE
═════════════════════════════════
Wallet W1: balance = 1000
Wallet W2: balance = 2000
Total: 3000

Ledger Entries: (none yet)


STATE DURING TRANSACTION
═════════════════════════════════
Created entries:
┌─────────────────────────────────────┐
│ Entry 1 (DEBIT)                     │
│ wallet_id: W1                       │
│ transfer_id: TXN-123                │
│ type: DEBIT                         │
│ amount: 100                         │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│ Entry 2 (CREDIT)                    │
│ wallet_id: W2                       │
│ transfer_id: TXN-123                │
│ type: CREDIT                        │
│ amount: 100                         │
└─────────────────────────────────────┘

INVARIANT CHECK:
SELECT SUM(amount) FROM ledger_entries
WHERE wallet_id = W1 AND type = 'DEBIT'
→ 100

SELECT SUM(amount) FROM ledger_entries
WHERE wallet_id = W1 AND type = 'CREDIT'
→ 0

W1 Balance: 1000 - 100 = 900 ✓

(Similar for W2: 2000 + 100 = 2100)


STATE AFTER COMMIT
═════════════════════════════════
Wallet W1: balance = 900
Wallet W2: balance = 2100
Total: 3000  ← PRESERVED!

Ledger verified:
SUM(DEBIT W1) = 100
SUM(CREDIT W2) = 100
SUM(CREDIT W1) = 0
SUM(DEBIT W2) = 0

INVARIANT SATISFIED: Total money in system unchanged!
```

---

## 6. Error Handling Decision Tree

```
                           Transfer Request
                                  │
                    ┌─────────────┴─────────────┐
                    │                           │
            Validation Error?              Service Call
             (amount ≤ 0,             (ExecuteTransfer)
              same wallet)                     │
                    │                  ┌──────┴──────┐
                    │                  │             │
                   YES               Idempotency   Transaction
                    │               Check          Error
                    │                 │             │
            HTTP 400                  │         ┌───┴────┐
            Bad Request               │         │        │
                                      │         │        │
                                    FOUND   Wallet   DB Error
                                      │     Not Found  (DB Down)
                                      │         │        │
                                      │      Rollback   Rollback
                                      │         │        │
                                      │    HTTP 422     HTTP 500
                                      │    Unprocessable Internal
                                      │                  Error
                                      │
                                  Return Cached
                                  (No DB write)
                                      │
                                   HTTP 200
                                   Success
                                   (Idempotent)

                                      │
                                  (If not found)
                                      │
                                   Execute
                                 Transaction
                                      │
                    ┌─────────────────┼─────────────────┐
                    │                 │                 │
                Insufficient      Wallet Not        Success
                Balance           Found              │
                    │                 │              ├─ All
                Rollback          Rollback       checks passed
                    │                 │           ├─ Balances
                HTTP 422         HTTP 422        updated
                                                 ├─ Ledger
                                                 entries
                                                 ├─ Idempotency
                                                 recorded
                                                 │
                                                 HTTP 200
                                                 OK
```

---

## 7. Database Schema Relationships

```
                 ┌──────────────────┐
                 │     wallets      │
                 │  ────────────────│
                 │ *wallet_id (PK)  │
                 │  balance         │
                 │  created_at      │
                 │  updated_at      │
                 └────────┬─────────┘
                          │
                  ┌───────┴───────┐
                  │               │
                  │ (from_wallet_id, indexed)
                  │ (to_wallet_id, indexed)
                  │               │
        ┌─────────▼───────────┐   │
        │    transfers        │   │
        │  ──────────────────│   │
        │ *transfer_id (PK)  │   │
        │  from_wallet_id    ├───┘
        │  to_wallet_id      │
        │  amount            │
        │  status            │
        │  created_at        │
        │  updated_at        │
        └────────┬────────────┘
                 │
        ┌────────▼────────────┐        ┌──────────────────────┐
        │  ledger_entries     │        │ idempotency_records  │
        │  ────────────────── │        │ ────────────────────│
        │ *entry_id (PK)      │        │*idempotency_key(PK)  │
        │  wallet_id (FK)  ───┼─┐      │ UNIQUE               │
        │  transfer_id (FK)   │ │      │ transfer_id (FK)     │
        │  type (DEBIT/CREDIT)│ │      │ response (JSONB)     │
        │  amount             │ │      │ created_at           │
        │  created_at         │ │      └──────────────────────┘
        └─────────────────────┘ │
                                │
                         (FK to transfers)
                                │
                    ┌───────────┘
                    │
                    └─────────────────────────────┐
                                                  │
        ┌──────────────────┐       ┌──────────────▼──────┐
        │     wallets      │       │    transfers        │
        └──────────────────┘       └─────────────────────┘
```

---

## 8. Performance Timeline

```
Request arrives
    │
    ├─ [~0.1ms] Parse JSON
    │
    ├─ [~0.5ms] Validate input
    │
    ├─ [~1ms]   Check idempotency
    │   │
    │   ├─ If found: return cached response
    │   │   └─ Total time: ~2ms ✓ (FAST PATH)
    │   │
    │   └─ If not found: proceed
    │
    ├─ [~2ms]   Begin transaction
    │
    ├─ [~3ms]   Create transfer record
    │
    ├─ [~5ms]   Lock source wallet (may wait if contended)
    │   └─ If lock wait: +10-100ms
    │
    ├─ [~1ms]   Verify balance
    │
    ├─ [~5ms]   Lock destination wallet
    │
    ├─ [~2ms]   Create DEBIT ledger entry
    │
    ├─ [~2ms]   Create CREDIT ledger entry
    │
    ├─ [~2ms]   Update source balance
    │
    ├─ [~2ms]   Update destination balance
    │
    ├─ [~1ms]   Update transfer status
    │
    ├─ [~1ms]   Record idempotency
    │
    ├─ [~5ms]   Commit transaction
    │
    └─ Total: ~35-50ms (fast path), ~15-25ms (cached)
       (without lock contention; higher under heavy concurrent load)
```

---

## 9. Concurrent Load Scenario

```
10 simultaneous transfers from wallet W1

Request 1: W1 → W2 (100)    │  Request 6: W1 → W7 (60)
Request 2: W1 → W3 (50)     │  Request 7: W1 → W8 (75)
Request 3: W1 → W4 (80)     │  Request 8: W1 → W9 (90)
Request 4: W1 → W5 (70)     │  Request 9: W1 → W10 (45)
Request 5: W1 → W6 (65)     │  Request 10: W1 → W11 (40)

Timeline:
═════════════════════════════════════════════════════════
Time │ Request 1  │ Request 2  │ Request 3  │ ... Others
─────┼────────────┼────────────┼────────────┼──────────────
 0ms │ BEGIN      │            │            │
     │ CREATE TXN1│            │            │
     │ LOCK W1    │ BEGIN      │            │
     │            │ CREATE TXN2│            │
     │            │ LOCK W1    │ (waiting)  │
     │            │ (wait)     │            │
─────┤            │            │            │
 5ms │ CHECK BAL  │            │            │
     │ LOCK W2    │            │            │
     │            │            │            │
─────┤            │            │            │
10ms │ UPDATE BAL │            │            │
     │ COMMIT     │ LOCK W1    │ (still     │
     │            │ (acquired) │ waiting)   │
     │ [W1-100]   │ CHECK BAL  │            │
─────┤            │ LOCK W3    │            │
15ms │            │ UPDATE BAL │ BEGIN      │
     │            │ COMMIT     │ CREATE TXN3│
     │            │ [W1-150]   │ LOCK W1    │
─────┤            │            │ (wait)     │
20ms │ DONE ✓     │ DONE ✓     │            │
     │            │            │ LOCK W1    │
     │            │            │ (acquired) │
─────┤            │            │            │
30ms │            │            │ CHECK BAL  │
     │            │            │ LOCK W4    │
─────┤            │            │ UPDATE BAL │
40ms │            │            │ COMMIT     │
     │            │            │ [W1-230]   │
     │            │            │ DONE ✓     │

Database end state:
W1 balance = 1000 - 100 - 50 - 80 - 70 - 65 - 60 - 75 - 90 - 45 - 40 = 225 ✓

All 10 transfers serialized by row-level lock on W1
No negative balance ever possible
Ledger entries = 20 (10 debits + 10 credits)
All idempotency keys stored for retry safety
```

---

## 10. State Machine - Transfer Lifecycle

```
                         TRANSFER STATES

┌──────────────────────────────────────────┐
│            CREATE TRANSFER               │
│    (in ExecuteTransfer function)         │
└────────────────┬─────────────────────────┘
                 │
                 ▼
        ┌─────────────────┐
        │    PENDING      │◄──── Initial state
        │  (DB inserted)  │      when created
        └────────┬────────┘
                 │
         ┌───────┴────────┐
         │                │
         ▼                ▼
    ┌──────────┐    ┌─────────────┐
    │PROCESSED │    │   FAILED    │
    │(Success) │    │(Error case) │
    └──────────┘    └─────────────┘
         │                │
         │          (Not implemented
         │           in current code
         │           but schema ready)
         │
    ┌────▼──────────────────────┐
    │  PERMANENT STATE           │
    │  - Cannot change after set │
    │  - Recorded in ledger      │
    │  - Idempotency tracked     │
    └────────────────────────────┘

Transitions:
═════════════════════════════════════════
PENDING → PROCESSED: ✓ All checks passed
PENDING → FAILED:    ✓ (Rollback on error)
PENDING → other:     ✗ Not allowed


Transfer Lifecycle Example:
═════════════════════════════════════════
Time  │ State        │ Event
──────┼──────────────┼────────────────────────
 0ms  │ (not exist)  │ Request arrives
      │              │
 5ms  │ PENDING      │ Created, inserted in DB
      │              │ (BEGIN TRANSACTION)
      │              │
15ms  │ PENDING      │ Locked wallets,
      │              │ verified balance
      │              │
20ms  │ PENDING      │ Ledger entries created,
      │              │ balances updated
      │              │
25ms  │ PROCESSED    │ Status updated to PROCESSED
      │              │ Idempotency recorded
      │              │ (COMMIT TRANSACTION)
      │              │
30ms  │ PROCESSED    │ Response sent to client
      │              │ Transaction complete
```

---

## Summary Table

| Aspect             | Mechanism                    | Benefit                        |
| ------------------ | ---------------------------- | ------------------------------ |
| **Concurrency**    | Row-level locks (FOR UPDATE) | No double-spending             |
| **Atomicity**      | Transaction wrapping         | All-or-nothing                 |
| **Idempotency**    | UNIQUE constraint + caching  | Exactly-once semantics         |
| **Correctness**    | Double-entry ledger          | Financial accuracy             |
| **Persistence**    | PostgreSQL ACID              | Durable state                  |
| **Query Speed**    | Balance cache + indexes      | O(1) balance, O(log n) history |
| **Audit Trail**    | Ledger entries               | Full transaction history       |
| **Validation**     | DB constraints               | Enforced at schema level       |
| **Error Recovery** | Automatic rollback           | No orphaned records            |
