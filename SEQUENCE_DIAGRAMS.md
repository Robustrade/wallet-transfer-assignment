# Sequence Diagrams

## 1. Happy Path: Successful Transfer

```
User          Handler         Service         Repository        Database
 │              │               │                  │               │
 ├─POST /transfers─────────────>│                  │               │
 │              │               │                  │               │
 │              ├─Validate────>│                  │               │
 │              │               │                  │               │
 │              │               ├─Check Idempotency─────────────>│
 │              │               │                  │<─Not Found───┤
 │              │               │                  │               │
 │              │               ├─BEGIN TRANSACTION─────────────>│
 │              │               │                  │<─OK───────────┤
 │              │               │                  │               │
 │              │               ├─Lock wallet_1────────────────>│
 │              │               │  (FOR UPDATE)   │<─Lock OK──────┤
 │              │               │                  │               │
 │              │               ├─Lock wallet_2────────────────>│
 │              │               │  (FOR UPDATE)   │<─Lock OK──────┤
 │              │               │                  │               │
 │              │               ├─Get wallet_1─────────────────>│
 │              │               │                  │<─Balance──────┤
 │              │               │                  │               │
 │              │               ├─Verify Balance──X               │
 │              │               │ (Balance >= amount? YES)        │
 │              │               │                  │               │
 │              │               ├─Create Transfer─────────────>│
 │              │               │                  │<─OK───────────┤
 │              │               │                  │               │
 │              │               ├─Update Balances──────────────>│
 │              │               │                  │<─OK───────────┤
 │              │               │                  │               │
 │              │               ├─Create Ledger Entries────────>│
 │              │               │  (DEBIT + CREDIT)│<─OK───────────┤
 │              │               │                  │               │
 │              │               ├─Record Idempotency────────────>│
 │              │               │                  │<─OK───────────┤
 │              │               │                  │               │
 │              │               ├─COMMIT───────────────────────>│
 │              │               │                  │<─OK───────────┤
 │              │               │                  │               │
 │              │<─201 Response─┤                  │               │
 │              │               │                  │               │
 │<─201 Created─│               │                  │               │
```

## 2. Idempotency: Cache Hit (Retry)

```
User          Handler         Service         Repository        Database
 │              │               │                  │               │
 ├─POST /transfers─────────────>│ (Same request)   │               │
 │              │ (same key)   │                  │               │
 │              │               │                  │               │
 │              ├─Validate────>│                  │               │
 │              │               │                  │               │
 │              │               ├─Check Idempotency─────────────>│
 │              │               │                  │<─FOUND!───────┤
 │              │               │                  │ (cached response)
 │              │               │                  │               │
 │              │               ├─Unmarshal Cache─X               │
 │              │               │ (same response)  │               │
 │              │               │                  │               │
 │              │<─201 Response─┤                  │               │
 │              │               │                  │               │
 │<─201 Created─│ (SAME response)                  │               │
 │              │ but NO new     │                  │               │
 │              │ transfer      │                  │               │
 │              │               │                  │               │
 Note: Database unchanged - exactly-once guarantee maintained!
```

## 3. Error Case: Insufficient Funds

```
User          Handler         Service         Repository        Database
 │              │               │                  │               │
 ├─POST /transfers─────────────>│                  │               │
 │              │               │                  │               │
 │              ├─Validate────>│                  │               │
 │              │               │                  │               │
 │              │               ├─Check Idempotency─────────────>│
 │              │               │                  │<─Not Found───┤
 │              │               │                  │               │
 │              │               ├─BEGIN TRANSACTION─────────────>│
 │              │               │                  │<─OK───────────┤
 │              │               │                  │               │
 │              │               ├─Lock wallet_1────────────────>│
 │              │               │                  │<─Lock OK──────┤
 │              │               │                  │               │
 │              │               ├─Lock wallet_2────────────────>│
 │              │               │                  │<─Lock OK──────┤
 │              │               │                  │               │
 │              │               ├─Get wallet_1─────────────────>│
 │              │               │                  │<─Balance──────┤
 │              │               │                  │  ($50)        │
 │              │               │                  │               │
 │              │               ├─Verify Balance──X               │
 │              │               │ (Balance >= amount? NO!)        │
 │              │               │                  │               │
 │              │               ├─ROLLBACK─────────────────────>│
 │              │               │                  │<─OK───────────┤
 │              │               │ (No changes made)               │
 │              │               │                  │               │
 │              │<─402 Error────┤                  │               │
 │              │               │                  │               │
 │<─402 Insufficient Funds──────│                  │               │
 │              │               │                  │               │
 Note: No transfer created, no balance updates, no ledger entries!
```

## 4. Concurrency: Two Concurrent Transfers (Race Prevention)

```
Thread A                        Thread B
(wallet_1 → wallet_2)          (wallet_1 → wallet_3)
│                               │
├─BEGIN SERIALIZABLE────────────┤
│  (tx_a)                       │
│                               ├─BEGIN SERIALIZABLE
│                               │  (tx_b)
│                               │
├─Lock wallet_1                 │
│ (gets exclusive lock)         │
│                               ├─Lock wallet_1
│                               │ (BLOCKED - waiting for tx_a)
│                               │
├─Check balance: $1000 ✓        │
│                               │
├─Debit $600 from wallet_1      │
│ (balance → $400)              │
│                               │
├─Update balance                │
│ (wallet_1 = $400)             │
│                               │
├─Create ledger entries         │
│ (DEBIT $600, CREDIT $600)    │
│                               │
├─COMMIT                        │
│ (releases lock)               │
│                               ├─Lock wallet_1 acquired
│                               │
│                               ├─Check balance: $400
│                               │ (NOT stale! SERIALIZABLE guarantee)
│                               │
│                               ├─Debit $600
│                               │ (FAILS! $400 < $600)
│                               │
│                               ├─ROLLBACK
│                               │ (no changes)

Result:
  TX_A: SUCCESS ($600 transferred)
  TX_B: FAILURE (insufficient funds)
  No double-spending ✓
  Balance correct: wallet_1 = $400 ✓
```

## 5. Concurrent Race on Idempotency Key

```
Thread A                        Thread B
(same request)                  (same request)
│                               │
├─Check idempotency             │
│  SELECT FROM idemp...         │
│  (not found yet)              │
│                               ├─Check idempotency
│                               │  SELECT FROM idemp...
│                               │  (not found yet)
│                               │
├─Execute transfer              │
│ BEGIN TRANSACTION             │
│                               ├─Execute transfer
│                               │ BEGIN TRANSACTION
│                               │
├─Create transfer t_xyz         │
│                               ├─Create transfer t_xyz
│                               │ (could be unique violation)
│                               │
├─Record idempotency            │
│  INSERT idemp_key = "abc123" │
│  (success - first INSERT)     │
│                               ├─Record idempotency
│                               │  INSERT idemp_key = "abc123"
│                               │  (UNIQUE CONSTRAINT VIOLATION!)
│                               │
├─COMMIT                        │
│ (transfer + idempotency)      │
│                               ├─ROLLBACK
│                               │ (failed to record)
│                               │
│                               ├─Client receives error OR retries
│                               │
On Retry:
├─Check idempotency
│  SELECT FROM idemp...
│  (FOUND! get cached response)
│                               │
├─Return cached response
│ (same transferId, same response)

Result:
  Transfer created: 1 (exactly once) ✓
  Both threads see same result ✓
```

## 6. Transfer State Machine Transitions

```
                    START
                      │
                      ▼
                  PENDING (initial)
                   ╱    ╲
                  ╱      ╲
                 ▼        ▼
            PROCESSED   FAILED
              (final)   (final)

Invalid transitions (rejected):
  PROCESSED → FAILED ✗
  PROCESSED → PENDING ✗
  FAILED → PROCESSED ✗
  FAILED → PENDING ✗

Valid transitions:
  PENDING → PROCESSED ✓ (on success)
  PENDING → FAILED ✓ (on error)
```

## 7. Double-Entry Ledger Flow

```
Transfer: wallet_a → wallet_b, amount = $500

Step 1: Create Transfer
  transfers:
    id=t_123, from=a, to=b, amount=50000, status=PENDING

Step 2: Mark Processed & Create Ledger
  transfers:
    id=t_123, from=a, to=b, amount=50000, status=PROCESSED ✓

  ledger_entries:
    id=e_d1, transfer=t_123, wallet=a, type=DEBIT, amount=50000
    id=e_c1, transfer=t_123, wallet=b, type=CREDIT, amount=50000

Invariant verified:
  Total DEBIT (wallet_a) = $500 ✓
  Total CREDIT (wallet_b) = $500 ✓
  DEBIT = CREDIT ✓ (Ledger balanced)

Balances:
  wallet_a: balance -= $500 ✓
  wallet_b: balance += $500 ✓
```

## 8. Lock Ordering to Prevent Deadlock

```
Two concurrent transfers with different wallet pairs:

Transfer 1: wallet_A → wallet_B
Transfer 2: wallet_B → wallet_A

With lock ordering (always lock smaller ID first):

Transfer 1:          Transfer 2:
Sort: A < B          Sort: A < B
Lock A               Lock A (waits)
Lock B               
Process              (Transfer 1 completes and releases)
Unlock B
Unlock A             Lock A (acquired)
                     Lock B
                     Process
                     Unlock B
                     Unlock A

Result: NO DEADLOCK ✓

Without consistent ordering (would deadlock):
Transfer 1:          Transfer 2:
Lock B               Lock A
Lock A (waits) ←─────→ Lock B (waits)
                  DEADLOCK ✗
```
