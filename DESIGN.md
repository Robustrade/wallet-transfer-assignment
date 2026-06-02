# Wallet Transfer Service - Design Document

## Overview

This document outlines the design and implementation of a reliable transactional wallet transfer service with guarantees for idempotency, concurrency safety, ledger consistency, and safe state transitions.

---

## Architecture

### Layered Architecture

The system follows a clean layered architecture with clear separation of concerns:

```
┌─────────────────────────────────────┐
│  Presentation Layer (Controller)    │  HTTP handlers, request validation, DTOs
├─────────────────────────────────────┤
│  Application Layer (Service)        │  Business logic, orchestration, idempotency
├─────────────────────────────────────┤
│  Infrastructure Layer (Repository)  │  Database persistence, transactions
├─────────────────────────────────────┤
│  Domain Layer (Entities)            │  Rich domain objects, validation, state machine
├─────────────────────────────────────┤
│  Database Layer (PostgreSQL)        │  Tables, constraints, indexes
└─────────────────────────────────────┘
```

### Package Structure

```
src/main/java/com/walletservice/
├── WalletTransferApplication.java      # Spring Boot entry point
├── presentation/                        # REST API layer
│   ├── controller/
│   │   └── TransferController.java
│   ├── dto/
│   │   ├── CreateTransferRequest.java
│   │   └── TransferResponse.java
│   └── exception/
│       └── GlobalExceptionHandler.java
├── application/                         # Business logic layer
│   ├── service/
│   │   └── TransferService.java
│   ├── exception/
│   │   └── IdempotencyConflictException.java
│   └── util/
│       └── HashUtil.java
├── infrastructure/                      # Persistence layer
│   └── repository/
│       ├── WalletRepository.java
│       ├── TransferRepository.java
│       ├── LedgerEntryRepository.java
│       └── IdempotencyRecordRepository.java
└── domain/                              # Domain models
    └── entity/
        ├── Wallet.java
        ├── Transfer.java
        ├── LedgerEntry.java
        └── IdempotencyRecord.java
```

---

## Database Design

### Schema

#### 1. Wallets Table

```sql
CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,  -- Optimistic locking
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

**Design Rationale:**
- `id`: UUID as primary key for global uniqueness
- `balance`: Stored balance for efficient queries (derived from ledger via views/calculations on demand)
- `version`: Optimistic locking version to detect concurrent modifications
- Immutable timestamps for audit trail

#### 2. Transfers Table

```sql
CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    version BIGINT NOT NULL DEFAULT 0,  -- Optimistic locking
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    INDEX idx_transfer_status (status),
    INDEX idx_transfer_from_wallet (from_wallet_id),
    INDEX idx_transfer_to_wallet (to_wallet_id)
);
```

**Design Rationale:**
- State machine: PENDING -> PROCESSED | FAILED
- `version`: Detects concurrent state transitions
- Indexes on status and wallet IDs for efficient queries

#### 3. Ledger Entries Table (Double-Entry Bookkeeping)

```sql
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    entry_type VARCHAR(50) NOT NULL,  -- DEBIT or CREDIT
    amount BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    INDEX idx_ledger_transfer (transfer_id),
    INDEX idx_ledger_wallet (wallet_id)
);
```

**Design Rationale:**
- Immutable historical record of all transactions
- Exactly 2 entries per transfer (1 debit, 1 credit)
- Enables balance verification via sum queries
- Enables audit trails and reconciliation

#### 4. Idempotency Records Table

```sql
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,  -- Globally unique
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    request_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL
);
```

**Design Rationale:**
- Primary key on `idempotency_key` ensures exactly one transfer per key
- Database constraint prevents duplicate inserts
- `request_hash` detects if key is reused with different parameters
- Enables exactly-once semantics at API level

### Key Constraints

1. **Unique idempotency key**: PRIMARY KEY constraint ensures one transfer per key
2. **Wallet existence**: FOREIGN KEY constraints ensure wallets exist
3. **Transfer existence**: FOREIGN KEY ensures ledger entries reference valid transfers
4. **Referential integrity**: Cascade rules prevent orphaned records

### Indexes

Indexes are created for common query patterns:
- `transfers.status`: For finding pending transfers
- `transfers.from_wallet_id`: For finding outgoing transfers
- `transfers.to_wallet_id`: For finding incoming transfers
- `ledger_entries.transfer_id`: For balance calculation by transfer
- `ledger_entries.wallet_id`: For wallet-specific ledger entries

---

## Concurrency Safety Strategy

### Problem: Race Conditions

Multiple concurrent requests can modify:
1. The same wallet balance (concurrent debits/credits)
2. The same transfer status (concurrent state transitions)
3. The same idempotency key (duplicate requests)

### Solution: Pessimistic Locking

We use **pessimistic (row-level) write locks** to serialize access:

```
Request 1                          Request 2
├─ LOCK wallet A (blocks)         ├─ Lock wallet A (waits)
├─ Read balance                   │
├─ Debit $100                     │
├─ Update balance                 │
├─ UNLOCK wallet A ───────────────┼──> LOCK wallet A (acquired)
                                  ├─ Read balance (updated)
                                  ├─ Debit $200
                                  ├─ Update balance
                                  ├─ UNLOCK wallet A
```

### Implementation

```java
@Lock(LockModeType.PESSIMISTIC_WRITE)
@Query("SELECT w FROM Wallet w WHERE w.id = :id")
Optional<Wallet> findByIdForUpdate(@Param("id") UUID id);
```

**Why Pessimistic Locking?**
- **Simplicity**: No complex retry logic or optimistic locking version checks
- **Correctness**: Guaranteed serialization for financial operations
- **Read consistency**: No stale reads or phantom reads
- **Performance**: For wallet transfers (relatively rare), lock overhead is acceptable

### Deadlock Prevention

Lock ordering prevents deadlock when transferring between wallets:

```java
// Always lock source first, then destination
Wallet sourceWallet = walletRepository.findByIdForUpdate(fromWalletId);
Wallet destinationWallet = walletRepository.findByIdForUpdate(toWalletId);
```

This consistent ordering prevents circular wait conditions.

### Transaction Boundaries

All operations within a single `@Transactional` method are atomic:

```
BEGIN TRANSACTION
├─ Check idempotency record
├─ Lock source wallet
├─ Lock destination wallet
├─ Create transfer (PENDING)
├─ Create ledger entries (DEBIT, CREDIT)
├─ Update wallet balances
├─ Mark transfer PROCESSED
├─ Record idempotency key
COMMIT or ROLLBACK
```

If any step fails, entire transaction rolls back.

---

## Idempotency Strategy

### Problem: Duplicate Requests

Network failures or retries can cause the same request to arrive multiple times:

```
Request 1 (idempotencyKey=abc123)  ──> Creates transfer T1 ──> Response 1
                                        ├─ Status: 201 Created
                                        └─ Body: { id: T1, ... }
                                             ↓ (Network failure - client doesn't receive)
Request 2 (idempotencyKey=abc123)  ──> Should return same response
```

### Solution: Idempotency Record Table

1. **First Request** (idempotencyKey=abc123):
   - Check `idempotency_records` table
   - No record found ✓
   - Create transfer T1
   - Record idempotency: INSERT idempotency_records(key=abc123, transfer_id=T1, hash=...)
   - Return transfer T1

2. **Duplicate Request** (idempotencyKey=abc123):
   - Check `idempotency_records` table
   - Record found: key→T1 mapping ✓
   - Verify request hash matches (detect misuse)
   - Return original transfer T1
   - No new transfer created

3. **Conflict Request** (idempotencyKey=abc123, different parameters):
   - Check `idempotency_records` table
   - Record found: key→T1 mapping ✓
   - Request hash differs ✗
   - Throw IdempotencyConflictException (409 Conflict)
   - Prevent misuse of idempotency keys

### Implementation

```java
// Step 1: Check idempotency
String requestHash = HashUtil.hashTransferRequest(...);
Optional<IdempotencyRecord> existing = idempotencyRecordRepository.findById(idempotencyKey);

if (existing.isPresent()) {
    if (!existing.get().getRequestHash().equals(requestHash)) {
        throw new IdempotencyConflictException(...);
    }
    return transferRepository.findById(existing.get().getTransferId());
}

// Step 2: Create transfer if new
Transfer transfer = transferRepository.save(...);

// Step 3: Record idempotency
IdempotencyRecord record = IdempotencyRecord.builder()
    .idempotencyKey(idempotencyKey)
    .transferId(transfer.getId())
    .requestHash(requestHash)
    .build();
idempotencyRecordRepository.save(record);
```

### Guarantees

- **Exactly-once semantics**: Same request always returns same result
- **Duplicate detection**: Prevents duplicate side effects (no duplicate transfers)
- **Conflict detection**: Reusing key with different parameters is rejected
- **Atomicity**: Recording idempotency key happens in same transaction as transfer creation

---

## Double-Entry Ledger Design

### Problem: Balance Correctness

Must guarantee that:
1. Every transfer creates entries for both source and destination
2. Debit and credit amounts are equal
3. Wallet balance = sum(credits) - sum(debits)

### Solution: Immutable Ledger Entries

For every transfer:
1. Create 1 DEBIT entry (decreases source wallet balance)
2. Create 1 CREDIT entry (increases destination wallet balance)
3. Both entries are immutable (no updates or deletes)
4. Database constraint ensures exactly 2 entries per transfer

### Implementation

```java
// Create ledger entries atomically with transfer
LedgerEntry debitEntry = LedgerEntry.builder()
    .transferId(transfer.getId())
    .walletId(sourceWalletId)
    .entryType(EntryType.DEBIT)
    .amount(amount)
    .build();
ledgerEntryRepository.save(debitEntry);

LedgerEntry creditEntry = LedgerEntry.builder()
    .transferId(transfer.getId())
    .walletId(destinationWalletId)
    .entryType(EntryType.CREDIT)
    .amount(amount)
    .build();
ledgerEntryRepository.save(creditEntry);

// Update balances
sourceWallet.debit(amount);
destinationWallet.credit(amount);
walletRepository.save(sourceWallet);
walletRepository.save(destinationWallet);
```

### Ledger Consistency Verification

Query to verify ledger balances all wallets:

```sql
SELECT 
    wallet_id,
    SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE 0 END) as total_credits,
    SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE 0 END) as total_debits,
    (SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE 0 END) -
     SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE 0 END)) as calculated_balance
FROM ledger_entries
GROUP BY wallet_id;
```

Compare `calculated_balance` with stored `wallet.balance` for reconciliation.

---

## Safe State Transitions

### Transfer State Machine

```
    ┌──────────────┐
    │   PENDING    │◄─── Initial state when transfer created
    └──────┬───────┘
           │
      Transfer processed successfully
           │
      ┌────▼──────────┐
      │  PROCESSED    │◄─── Terminal state: transfer completed
      └───────────────┘

    OR

    ┌──────────────┐
    │   PENDING    │
    └──────┬───────┘
           │
      Transfer processing fails
           │
      ┌────▼──────────┐
      │   FAILED      │◄─── Terminal state: transfer rolled back
      └───────────────┘
```

### Transition Validation

Each transition is validated:

```java
public void markProcessed() {
    if (this.status != TransferStatus.PENDING) {
        throw new IllegalStateException(
            "Transfer must be in PENDING state to process. Current state: " + this.status);
    }
    this.status = TransferStatus.PROCESSED;
}

public void markFailed() {
    if (this.status != TransferStatus.PENDING) {
        throw new IllegalStateException(
            "Transfer must be in PENDING state to fail. Current state: " + this.status);
    }
    this.status = TransferStatus.FAILED;
}
```

### Concurrency-Safe Transitions

Using pessimistic locking:

```java
Transfer transfer = transferRepository.findByIdForUpdate(transferId);
transfer.validateCanProcess();  // Throws if not PENDING
transfer.markProcessed();
transferRepository.save(transfer);
```

The lock ensures only one thread can transition the status.

### Retry Safety

If processing fails after transfer is created:

1. Transfer remains in PENDING state
2. No ledger entries or balance updates were made
3. Retry operation can start fresh
4. Idempotency key prevents duplicate if request is retried

---

## Error Handling

### Exception Hierarchy

```
Exception
├── IllegalArgumentException          # Invalid input (wallet not found, invalid amount)
├── IllegalStateException             # Invalid state (insufficient balance, bad transition)
├── IdempotencyConflictException      # Idempotency key reused with different params
└── RuntimeException
```

### HTTP Status Codes

| Scenario | Status Code | Message |
|----------|------------|---------|
| Successfully created transfer | 201 Created | Transfer details |
| Invalid request body | 400 Bad Request | Validation error |
| Wallet not found | 400 Bad Request | "Wallet not found" |
| Idempotency key conflict | 409 Conflict | "Idempotency conflict" |
| Insufficient balance | 422 Unprocessable Entity | "Insufficient balance" |
| Server error | 500 Internal Server Error | Generic error message |

### Global Exception Handler

```java
@RestControllerAdvice
public class GlobalExceptionHandler {
    @ExceptionHandler(IdempotencyConflictException.class)
    public ResponseEntity<Map<String, Object>> handleIdempotencyConflict(...) {
        return ResponseEntity.status(HttpStatus.CONFLICT).body(...);
    }
    
    @ExceptionHandler(IllegalStateException.class)
    public ResponseEntity<Map<String, Object>> handleIllegalState(...) {
        return ResponseEntity.status(HttpStatus.UNPROCESSABLE_ENTITY).body(...);
    }
}
```

---

## Testing Strategy

### Unit Tests (TransferServiceTest)

- Test service business logic in isolation
- Mock repositories to verify correct calls
- Test idempotency behavior
- Test error cases
- Test state transitions

### Integration Tests (TransferServiceIntegrationTest)

- Test with real database (H2)
- Verify idempotency at database level
- Test concurrent transfers
- Verify ledger consistency
- Test transaction rollback on failure

### Controller Tests (TransferControllerIntegrationTest)

- Test REST API endpoints
- Verify request validation
- Test error responses
- Test idempotency behavior through HTTP

### Concurrency Tests

```java
ExecutorService executor = Executors.newFixedThreadPool(3);
// Submit 3 concurrent transfers from same wallet
// Verify balances are correct and no double-spending
```

---

## API Specification

### Create Transfer

```
POST /api/v1/transfers
Content-Type: application/json

{
  "idempotencyKey": "abc123",
  "fromWalletId": "550e8400-e29b-41d4-a716-446655440000",
  "toWalletId": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
  "amount": 100
}

Response: 201 Created
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "fromWalletId": "550e8400-e29b-41d4-a716-446655440000",
  "toWalletId": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
  "amount": 100,
  "status": "PROCESSED",
  "createdAt": "2024-05-28T10:30:00",
  "updatedAt": "2024-05-28T10:30:00"
}
```

### Get Transfer

```
GET /api/v1/transfers/{transferId}

Response: 200 OK
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "fromWalletId": "550e8400-e29b-41d4-a716-446655440000",
  "toWalletId": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
  "amount": 100,
  "status": "PROCESSED",
  "createdAt": "2024-05-28T10:30:00",
  "updatedAt": "2024-05-28T10:30:00"
}
```

---

## Key Design Decisions and Tradeoffs

### 1. Pessimistic Locking vs. Optimistic Locking

**Decision**: Pessimistic locking with PESSIMISTIC_WRITE

**Rationale**:
- Financial transactions require strong consistency guarantees
- Pessimistic locking is simpler and prevents race conditions at source
- Wallet transfers are relatively rare (low contention)
- No complex retry logic needed
- Prevents lost updates better than optimistic locking

**Tradeoff**: Lower throughput under high contention, but guaranteed correctness

### 2. Stored Balance vs. Derived Balance

**Decision**: Stored balance with ledger verification

**Rationale**:
- Stored balance enables efficient balance queries without aggregation
- Ledger entries still exist as immutable historical record
- Can verify balance correctness by comparing stored vs. calculated
- Better read performance

**Tradeoff**: Must keep stored balance in sync with ledger

### 3. Idempotency Strategy

**Decision**: Database constraint + idempotency table + request hash

**Rationale**:
- Database primary key constraint is the strongest guarantee
- Request hash detects accidental key reuse with different parameters
- Prevents duplicate side effects
- Exactly-once semantics at API level
- Works correctly with network failures and retries

**Tradeoff**: Extra database insert for idempotency record

### 4. Transaction Boundaries

**Decision**: All transfer operations in single @Transactional method

**Rationale**:
- Atomicity: All or nothing
- Consistency: Ledger always balanced
- Isolation: Pessimistic locks prevent concurrent conflicts
- Durability: Database transaction guarantees
- Simpler to reason about

**Tradeoff**: Longer transaction might impact throughput

### 5. State Machine Design

**Decision**: Simple PENDING -> PROCESSED/FAILED state machine

**Rationale**:
- Most common pattern for financial transactions
- Pessimistic locking ensures safe transitions
- Easy to understand and verify
- Supports idempotency (state transition is idempotent)

---

### 6. Sample API
**Get Transfer Details**:
```
curl --location --request GET 'http://localhost:8080/api/v1/transfers/19f2fe84-d3d8-4fe7-b4e6-02d423d89510' \
--header 'Content-Type: application/json' \
--data '{
"idempotencyKey": "bb37c6fd-7d72-445f-83ae-36aaaafa5c48",
"fromWalletId": "7dd4deb6-3343-41ac-b405-e4040672efb6",
"toWalletId": "e8773e60-e8d6-4565-af64-e4adffdf5a51",
"amount": 5
}'
```

**Perform Transfer**:
```
curl --location 'http://localhost:8080/api/v1/transfers' \
--header 'Content-Type: application/json' \
--data '{
    "idempotencyKey": "bb37c6fd-7d72-445f-83ae-36aaaafa5c48",
    "fromWalletId": "7dd4deb6-3343-41ac-b405-e4040672efb6",
    "toWalletId": "e8773e60-e8d6-4565-af64-e4adffdf5a51",
    "amount": 5
}'
```

## Assumptions

1. **Balance precision**: Using BIGINT for amounts (assuming integer cents, not decimals)
2. **Wallet existence**: Wallets are created separately; transfer service assumes they exist
3. **Concurrency**: Expected moderate concurrency (pessimistic locking appropriate)
4. **No transfers without idempotencyKey**: All transfers must be idempotent
5. **Database ACID guarantees**: PostgreSQL with SERIALIZABLE or READ_COMMITTED isolation

---

## Future Enhancements

1. **Wallet Balance API**: GET /api/v1/wallets/{walletId}/balance
2. **Transfer History**: GET /api/v1/wallets/{walletId}/transfers
3. **Bulk Transfers**: Support multiple transfers in single request
4. **Scheduled Transfers**: Support future-dated transfers
5. **Transfer Cancellation**: Support canceling PENDING transfers
6. **Ledger Reporting**: SQL views for ledger reporting
7. **Metrics**: Prometheus metrics for transfer volumes, latencies
8. **Event Publishing**: Publish TransferCreated, TransferProcessed events
9. **Webhooks**: Notify external systems of transfer completion
10. **Rate Limiting**: Prevent abuse with rate limiters

---

## Monitoring and Observability

### Logging Levels

- **INFO**: Transfer creation, completion
- **DEBUG**: Wallet locking, balance updates, ledger entries
- **WARN**: Idempotency conflicts, insufficient balance
- **ERROR**: Transaction failures, unexpected errors

### Key Metrics

- Transfers created per minute
- Transfer success rate
- Average transfer processing time
- Duplicate request rate (idempotency collisions)
- Ledger consistency score

### Health Checks

- Database connectivity
- Ledger consistency (stored balance vs. calculated)
- Active lock monitoring

---

## Conclusion

This design prioritizes **correctness and reliability** over raw performance:

✓ **Idempotency**: Exactly-once semantics with database constraints
✓ **Concurrency**: Pessimistic locking prevents race conditions
✓ **Consistency**: Double-entry ledger ensures balance correctness
✓ **Simplicity**: Clear layered architecture with separated concerns
✓ **Testability**: Comprehensive unit and integration tests
✓ **Observability**: Logging and error handling throughout

The system is ready for production use in financial contexts where correctness is paramount.

