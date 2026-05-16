# Architecture & Design

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     HTTP REST API Layer                         │
│  - TransferHandler: POST /transfers, GET /transfers/{id}        │
│  - WalletHandler: GET /wallets/{id}, GET /wallets/{id}/balance  │
└──────────────────┬──────────────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────────────┐
│                    Service Layer (Business Logic)               │
│  - TransferService: orchestrates transfer workflow              │
│    * Idempotency check                                          │
│    * SERIALIZABLE transaction management                        │
│    * Balance validation                                         │
│    * Ledger recording                                           │
│  - WalletService: wallet operations                             │
└──────────────────┬──────────────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────────────┐
│                  Repository Layer (Data Access)                 │
│  - WalletRepository: CRUD + row locking (FOR UPDATE)            │
│  - TransferRepository: transfer persistence                     │
│  - LedgerRepository: double-entry ledger recording              │
│  - IdempotencyRepository: idempotency key storage               │
└──────────────────┬──────────────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────────────┐
│                   PostgreSQL Database                           │
│  - wallets: balance tracking                                    │
│  - transfers: state machine                                     │
│  - ledger_entries: double-entry bookkeeping                     │
│  - idempotency_records: exactly-once semantics                  │
└─────────────────────────────────────────────────────────────────┘
```

## Concurrency Strategy

### SERIALIZABLE Isolation with Row-Level Locks

All transfers execute in **SERIALIZABLE** isolation transactions:
- Strongest isolation level in PostgreSQL
- Prevents phantom reads, non-repeatable reads, dirty reads
- Row-level locks (FOR UPDATE) prevent concurrent modifications

### Lock Ordering to Prevent Deadlock

```go
// Always lock wallets in consistent order (by ID)
if walletID1 < walletID2 {
    lockWallet(walletID1)
    lockWallet(walletID2)
} else {
    lockWallet(walletID2)
    lockWallet(walletID1)
}
```

**Why this works:**
- No circular wait: All transactions acquire locks in same order
- Prevents deadlock even with 100s of concurrent transfers
- Serializable isolation ensures no race conditions

### Concurrency Scenario

```
Scenario: Two concurrent transfers from same wallet (insufficient funds)

Initial state: Wallet A = $1000

Transfer T1:                          Transfer T2:
1. Lock Wallet A                      1. Lock Wallet A (waits)
2. Check balance: $1000 ✓             
3. Debit $600                         2. (now acquires lock)
4. Commit                             3. Check balance: $400
                                      4. Debit $600 → FAILS ✓
```

Result: Only T1 succeeds, T2 correctly rejected. **No double-spending!**

## Idempotency Strategy

### Exactly-Once Semantics

**Problem:** Network failure after transfer but before response

```
Request: Create transfer with idempotencyKey="abc123"
Server: Creates transfer T1, returns response
Network: Connection lost → response not received
Client: Retries same request
Server: Must return SAME response, NOT create new transfer
```

**Solution:** Idempotency table

```
idempotency_records:
  PRIMARY KEY idempotencyKey
  - transfer_id: points to original transfer
  - response_body: cached response (JSON)
  - created_at: timestamp
```

**Flow:**

1. Client sends request with `idempotencyKey`
2. Server checks `idempotency_records` table
3. If found: Return cached response (no new transfer)
4. If not found:
   - Begin transaction
   - Execute transfer
   - INSERT into `idempotency_records` (atomic with transfer)
   - Commit (both transfer and idempotency record succeed or both fail)
5. On retry: Cache hit, return same response

### Why This Works

- **Unique constraint** on `idempotencyKey` prevents race conditions
- **Atomic insert** with transfer: Either both persist or neither
- **Survives process restart**: Idempotency record persists in DB
- **Cache-safe**: Can return same response indefinitely

## Database Schema

### Wallets Table
```sql
CREATE TABLE wallets (
    wallet_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    balance_cents BIGINT NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT balance_non_negative CHECK (balance_cents >= 0)
);
```

**Key constraints:**
- `balance_cents >= 0`: Prevents negative balances (enforced at DB level)
- `balance_cents BIGINT`: Supports up to $92 billion (in cents)
- Index on `user_id` for user queries

### Transfers Table
```sql
CREATE TABLE transfers (
    transfer_id UUID PRIMARY KEY,
    from_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    amount_cents BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    error_reason VARCHAR(500),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT amount_positive CHECK (amount_cents > 0),
    CONSTRAINT different_wallets CHECK (from_wallet_id != to_wallet_id)
);
```

**Key constraints:**
- `amount_cents > 0`: Prevents zero/negative transfers
- `from_wallet_id != to_wallet_id`: Prevents self-transfers
- `status IN (...)`: Only valid states allowed
- Foreign keys: Referential integrity

### Ledger Entries Table (Double-Entry Bookkeeping)
```sql
CREATE TABLE ledger_entries (
    entry_id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id),
    wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount_cents BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

**Invariant:** For every transfer, sum(DEBIT) = sum(CREDIT) = transfer.amount

### Idempotency Records Table
```sql
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id),
    response_body JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (idempotency_key)
);
```

**Key design:**
- Primary key on `idempotencyKey` enables fast lookups
- JSONB response: Flexible schema, queryable
- FK to transfer: Can audit transfers by idempotency key

## Transaction Flow

### CreateTransfer Request

```
1. Receive request with {idempotencyKey, fromWallet, toWallet, amount}

2. Validate:
   ✓ idempotencyKey not empty
   ✓ wallets specified
   ✓ wallets != same
   ✓ amount > 0

3. Check idempotency:
   IF idempotencyKey exists IN idempotency_records THEN
      RETURN cached_response (DONE)
   END

4. BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE

5. Lock wallets in order:
   SELECT * FROM wallets WHERE id=? FOR UPDATE
   (First wallet, then second wallet - prevents deadlock)

6. Validate business rules:
   ✓ from_wallet exists
   ✓ to_wallet exists
   ✓ from_wallet.balance >= amount

7. Create transfer:
   INSERT INTO transfers (id, from_wallet, to_wallet, amount, status)
   VALUES (?, ?, ?, ?, 'PROCESSED')

8. Update balances:
   UPDATE wallets SET balance = balance - amount WHERE id = from_wallet
   UPDATE wallets SET balance = balance + amount WHERE id = to_wallet

9. Record ledger entries (2 records):
   INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
   VALUES (?, transfer_id, from_wallet, 'DEBIT', amount)
   INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
   VALUES (?, transfer_id, to_wallet, 'CREDIT', amount)

10. Record idempotency:
    INSERT INTO idempotency_records (key, transfer_id, response)
    VALUES (idempotencyKey, transfer_id, response_json)

11. COMMIT (all operations succeed atomically)

12. RETURN {transferId, status='PROCESSED', amount, timestamp}
```

## Error Handling Strategy

### Error Hierarchy

```
400 Bad Request
  └─ INVALID_REQUEST: Malformed JSON
  └─ INVALID_AMOUNT: Amount <= 0
  └─ INVALID_WALLET: Wallet ID missing/invalid
  └─ SELF_TRANSFER: from == to
  └─ MISSING_IDEMPOTENCY_KEY: Required for exactly-once

402 Payment Required
  └─ INSUFFICIENT_FUNDS: Balance < amount

404 Not Found
  └─ WALLET_NOT_FOUND: Wallet doesn't exist
  └─ TRANSFER_NOT_FOUND: Transfer doesn't exist

409 Conflict
  └─ DUPLICATE_TRANSFER: Idempotency key already processed

500 Internal Server Error
  └─ DATABASE_ERROR: SQL execution failed
  └─ TRANSACTION_FAILED: Transaction commit/rollback failed
  └─ INTERNAL_ERROR: Unexpected error
```

### Error Response Format

```json
{
  "code": "INSUFFICIENT_FUNDS",
  "message": "Source wallet has insufficient balance",
  "details": {
    "walletId": "wallet_1",
    "balance": 500,
    "requested": 1000
  }
}
```

## Observability & Logging

### Log Levels

- **DEBUG**: Detailed transaction steps, lock acquisitions
- **INFO**: Transfer created, transfer processed, wallet balance
- **WARN**: Insufficient funds, invalid state
- **ERROR**: Database failures, transaction failures

### Log Format

```
[2025-01-15T10:30:45.123Z] INFO transfer created successfully transferId=t_123 amount=1000 fromWallet=w_1 toWallet=w_2
[2025-01-15T10:30:46.456Z] WARN insufficient funds walletId=w_3 balance=100 requestedAmount=500
[2025-01-15T10:30:47.789Z] INFO idempotency key found, returning cached response idempotencyKey=abc123 transferId=t_123
```

### Metrics (Recommended)

- `transfer_created_total`: Count of transfers created
- `transfer_duration_seconds`: Time to process transfer
- `concurrent_transfers`: Gauge of in-flight transfers
- `idempotency_cache_hits`: Duplicate requests detected
- `insufficient_funds_errors`: Balance validation failures
- `database_transaction_errors`: Transaction failures
- `latency_p50`, `latency_p95`, `latency_p99`: Performance percentiles

## Clean Architecture Principles

### Separation of Concerns

```
Domain Layer (No dependencies)
  └─ Entities: Wallet, Transfer, LedgerEntry
  └─ Business rules: Transfer state machine
  └─ Value objects: TransferStatus enum

Repository Layer (Depends on: Domain)
  └─ Database queries
  └─ Transaction management
  └─ Persistence logic

Service Layer (Depends on: Domain + Repository)
  └─ Business workflows
  └─ Idempotency orchestration
  └─ Error handling

Handler Layer (Depends on: Service + DTOs)
  └─ HTTP request/response mapping
  └─ Status code conversion
  └─ Input validation
```

### Dependency Direction

```
Handler → Service → Repository → Domain ← Domain Models
         ↘ DTOs (Data Transfer Objects)
```

**Note:** Domain layer has NO dependencies on other layers.

## Scalability Considerations

### Current Bottlenecks

1. **Row locks on wallets**: Serializes transfers from same wallet
   - Mitigation: Shard by wallet, use eventual consistency
   
2. **Single database**: Single point of failure
   - Mitigation: Add replication, failover

3. **Synchronous idempotency check**: Extra DB round-trip
   - Mitigation: Cache idempotency in-memory with TTL

### Future Optimizations

- Add connection pooling config
- Implement read replicas for read-only operations
- Cache frequently accessed wallets (with TTL)
- Use batch inserts for ledger entries
- Add CQRS pattern for read-heavy queries
- Implement saga pattern for multi-wallet transfers
