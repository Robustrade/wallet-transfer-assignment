# Implementation Guide

## Transaction Flow Explained

### Step-by-Step: CreateTransfer Operation

```
User Request:
POST /transfers
{
  "idempotencyKey": "payment-001",
  "fromWalletId": "wallet_alice",
  "toWalletId": "wallet_bob",
  "amount": 50000  // $500.00
}
│
├─ [Handler] Validate & parse JSON
│  └─ Check: amount > 0? ✓
│  └─ Check: fromWallet != toWallet? ✓
│  └─ Check: idempotencyKey provided? ✓
│
├─ [Service] Check idempotency
│  └─ Query: SELECT * FROM idempotency_records WHERE key = 'payment-001'
│  └─ Result: No record found (first request)
│
├─ [Service] Begin SERIALIZABLE transaction
│  └─ BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE
│
├─ [Service] Lock wallets in order
│  └─ wallet_alice < wallet_bob? (assume yes)
│  └─ SELECT * FROM wallets WHERE id='wallet_alice' FOR UPDATE
│     (Acquires exclusive lock on alice's row)
│  └─ SELECT * FROM wallets WHERE id='wallet_bob' FOR UPDATE
│     (Acquires exclusive lock on bob's row)
│
├─ [Service] Validate state
│  └─ wallet_alice.balance = $1000.00 (50000 cents)
│  └─ wallet_alice.balance >= amount? (50000 >= 50000)? ✓
│
├─ [Repository] Create transfer record
│  └─ INSERT INTO transfers (id, from_wallet, to_wallet, amount, status)
│     VALUES ('t_12345', 'wallet_alice', 'wallet_bob', 50000, 'PENDING')
│  └─ (Now: Transfer exists with status PENDING)
│
├─ [Repository] Update alice's balance
│  └─ UPDATE wallets SET balance = balance - 50000 WHERE id='wallet_alice'
│  └─ (alice: $1000 → $500)
│
├─ [Repository] Update bob's balance
│  └─ UPDATE wallets SET balance = balance + 50000 WHERE id='wallet_bob'
│  └─ (bob: $500 → $1000)
│
├─ [Repository] Create debit ledger entry
│  └─ INSERT INTO ledger_entries (id, transfer_id, wallet, type, amount)
│     VALUES ('le_d1', 't_12345', 'wallet_alice', 'DEBIT', 50000)
│
├─ [Repository] Create credit ledger entry
│  └─ INSERT INTO ledger_entries (id, transfer_id, wallet, type, amount)
│     VALUES ('le_c1', 't_12345', 'wallet_bob', 'CREDIT', 50000)
│
├─ [Repository] Update transfer status to PROCESSED
│  └─ UPDATE transfers SET status='PROCESSED' WHERE id='t_12345'
│
├─ [Repository] Record idempotency key
│  └─ INSERT INTO idempotency_records
│     (key, transfer_id, response_body)
│     VALUES (
│       'payment-001',
│       't_12345',
│       {"transferId":"t_12345","status":"PROCESSED",...}
│     )
│
├─ [Service] Commit transaction
│  └─ COMMIT
│  └─ (All changes persisted atomically)
│  └─ (Locks released)
│
└─ [Handler] Return response
   Response: 201 Created
   {
     "transferId": "t_12345",
     "status": "PROCESSED",
     "amount": 50000,
     "createdAt": "2025-01-15T10:30:45Z"
   }
```

## Concurrency Safety: Lock Ordering

### Deadlock Prevention

**Problem:** Without lock ordering, transfers can deadlock

```
Scenario WITHOUT lock ordering:

Thread 1: wallet_a → wallet_b
  Step 1: Lock wallet_a
  Step 2: Try to lock wallet_b (succeeds)
  
Thread 2: wallet_b → wallet_a (CONCURRENT)
  Step 1: Lock wallet_b
  Step 2: Try to lock wallet_a (BLOCKED by Thread 1)
  
Thread 1: Try to... wait, what?
  Already has wallet_a
  Needs wallet_b but Thread 2 has it
  DEADLOCK! ✗
```

**Solution: Consistent Lock Ordering**

```go
// Always lock in same order (sorted by ID)
func lockWalletsInOrder(ctx, tx, walletID1, walletID2) {
    if walletID1 < walletID2 {
        lock(walletID1)
        lock(walletID2)
    } else {
        lock(walletID2)
        lock(walletID1)
    }
}

// Now all transfers lock in same order
Thread 1: wallet_a → wallet_b
  Step 1: ID sort: a < b? yes
  Step 2: Lock a
  Step 3: Lock b ✓

Thread 2: wallet_b → wallet_a (CONCURRENT)
  Step 1: ID sort: b < a? no
  Step 2: Lock a (BLOCKED by Thread 1)
  Step 3: (waits)
  
Thread 1 completes:
  Step 4: Release locks
  
Thread 2 acquires:
  Step 2: Lock a ✓
  Step 3: Lock b ✓
  Step 4: Proceed ✓
```

## Idempotency Guarantee

### Cache Hit Scenario

```
Request 1 (Initial):
POST /transfers
Body: {"idempotencyKey":"key-123", "fromWalletId":"w1", "toWalletId":"w2", "amount":1000}

Server Processing:
  1. Check idempotency_records for "key-123"
  2. Not found → proceed
  3. Create transfer → returns transfer_id = "t_123"
  4. Record in idempotency_records: ("key-123", "t_123", {...response...})
  5. Return 201 with transfer data

Response 1: 201 Created
{"transferId":"t_123", "status":"PROCESSED"}

(Network timeout → response lost)

Request 2 (Retry):
POST /transfers
Body: {"idempotencyKey":"key-123", ...same...}

Server Processing:
  1. Check idempotency_records for "key-123"
  2. FOUND! ("key-123", "t_123", {...response...})
  3. Unmarshal cached response
  4. Return 201 with SAME response
  (No new transfer created!)

Response 2: 201 Created
{"transferId":"t_123", "status":"PROCESSED"}  // ← SAME as Response 1

Database State After Both Requests:
  transfers table: 1 record (t_123) ✓
  ledger_entries: 2 records (1 debit + 1 credit) ✓
  Exactly-once guarantee achieved! ✓
```

## Error Handling & Recovery

### Insufficient Funds

```
Request:
POST /transfers
{
  "idempotencyKey": "payment-002",
  "fromWalletId": "wallet_poor",
  "toWalletId": "wallet_rich",
  "amount": 100000  // $1000
}

Database State:
wallet_poor.balance = 5000  // $50

Server Processing:
  1. BEGIN TRANSACTION
  2. Lock wallet_poor
  3. Check: 5000 >= 100000? NO ✗
  4. ROLLBACK (no changes made)
  5. Return error

Response: 402 Payment Required
{
  "code": "INSUFFICIENT_FUNDS",
  "message": "Source wallet has insufficient balance",
  "details": {
    "walletId": "wallet_poor",
    "balance": 5000,
    "requested": 100000
  }
}

Database State After:
  No transfer created ✓
  No balance updates ✓
  Ledger entries unchanged ✓
  Idempotency record NOT created (error response) ✓
```

### Duplicate Request (Conflict)

```
First Request:
POST /transfers {"idempotencyKey":"key-444", ...}
Server: Creates transfer, records idempotency key
Response: 201 {"transferId":"t_444"}

Concurrent Request (Race):
POST /transfers {"idempotencyKey":"key-444", ...}
(arrives while first request still processing)

Timeline:
  T1: Thread A - checks idempotency → not found (just checking)
  T2: Thread B - checks idempotency → not found
  T3: Thread A - tries to INSERT idempotency key with key="key-444"
  T4: Thread B - tries to INSERT idempotency key with key="key-444"
      (PostgreSQL UNIQUE constraint violation!)
  T5: Thread A's INSERT succeeds
  T6: Thread B's INSERT fails → unique constraint violation
  
Result:
  Thread A: Creates transfer, response sent
  Thread B: Detects duplicate during INSERT → returns error OR retries
  
Transfer Created: 1 (exactly once) ✓
```

## Testing Concurrency

### Race Condition Test

```go
func TestConcurrentTransfersFromSameWallet(t *testing.T) {
    // Setup: wallet with $10,000
    setWalletBalance(t, "wallet_1", 1000000)
    
    numTransfers := 100
    var wg sync.WaitGroup
    results := make(chan error, numTransfers)
    
    // Launch 100 concurrent transfers
    for i := 0; i < numTransfers; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            request := CreateTransferRequest{
                IdempotencyKey: fmt.Sprintf("transfer-%d", i),
                FromWalletId:   "wallet_1",
                ToWalletId:     fmt.Sprintf("wallet_%d", i),
                Amount:         10000,
            }
            _, err := service.CreateTransfer(context.Background(), request)
            results <- err
        }(i)
    }
    
    wg.Wait()
    close(results)
    
    // Verify results
    successCount := 0
    failureCount := 0
    for err := range results {
        if err == nil {
            successCount++
        } else {
            failureCount++
        }
    }
    
    // Verify invariants
    assert.Equal(t, 100, successCount)  // All succeeded (had enough funds)
    assert.Equal(t, 0, failureCount)
    
    // Verify final balance
    finalBalance := getWalletBalance(t, "wallet_1")
    expectedBalance := 1000000 - (100 * 10000)
    assert.Equal(t, expectedBalance, finalBalance)
    
    // Verify ledger balance
    totalDebits := getTotalDebits(t, "wallet_1")
    totalCredits := getTotalCredits(t, "wallet_1")
    assert.Equal(t, 100*10000, totalDebits)   // All transfers debited
    assert.Equal(t, 0, totalCredits)          // No credits to this wallet
}
```

## Database Verification Queries

### Check Ledger Balance

```sql
-- Verify every transfer has balanced ledger entries
SELECT 
    t.transfer_id,
    t.amount_cents,
    SUM(CASE WHEN le.entry_type = 'DEBIT' THEN le.amount_cents ELSE 0 END) as total_debit,
    SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_cents ELSE 0 END) as total_credit
FROM transfers t
LEFT JOIN ledger_entries le ON t.transfer_id = le.transfer_id
GROUP BY t.transfer_id
HAVING 
    SUM(CASE WHEN le.entry_type = 'DEBIT' THEN le.amount_cents ELSE 0 END) != t.amount_cents
    OR SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_cents ELSE 0 END) != t.amount_cents;

-- If query returns rows, ledger is corrupted! (Should return 0 rows)
```

### Check Wallet Balances Match Ledger

```sql
-- Verify wallet balance matches ledger sum
SELECT 
    w.wallet_id,
    w.balance_cents as stored_balance,
    SUM(CASE 
        WHEN le.entry_type = 'CREDIT' THEN le.amount_cents
        WHEN le.entry_type = 'DEBIT' THEN -le.amount_cents
        ELSE 0
    END) as calculated_balance
FROM wallets w
LEFT JOIN ledger_entries le ON w.wallet_id = le.wallet_id
GROUP BY w.wallet_id, w.balance_cents
HAVING w.balance_cents != SUM(CASE 
    WHEN le.entry_type = 'CREDIT' THEN le.amount_cents
    WHEN le.entry_type = 'DEBIT' THEN -le.amount_cents
    ELSE 0
END);

-- If query returns rows, balances are inconsistent! (Should return 0 rows)
```

### Check Idempotency Coverage

```sql
-- Verify most transfers have idempotency records
SELECT 
    COUNT(*) as total_transfers,
    COUNT(ir.idempotency_key) as transfers_with_idempotency,
    ROUND(100.0 * COUNT(ir.idempotency_key) / COUNT(*), 2) as coverage_percent
FROM transfers t
LEFT JOIN idempotency_records ir ON t.transfer_id = ir.transfer_id;
```
