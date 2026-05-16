# Wallet Transfer Assignment — Comprehensive Analysis

## 1. Expected Deliverables

The assignment requires building a **wallet-to-wallet transfer service** that demonstrates core financial systems engineering:

| Deliverable | Details |
|---|---|
| **POST /transfers endpoint** | Accept transfer requests with idempotency keys, source/destination wallets, and amount |
| **Wallet balance management** | Maintain accurate balances either derived or cached |
| **Double-entry ledger** | Record both DEBIT and CREDIT entries for every transfer |
| **State machine** | PENDING → PROCESSED/FAILED transitions |
| **Idempotency guarantee** | Exactly-once semantics with duplicate detection |
| **Concurrency safety** | Prevent race conditions, double-spending, ledger corruption |
| **Comprehensive tests** | Unit, service, and concurrency tests |
| **Clean architecture** | Handler → Service → Repository → Domain layers |

---

## 2. Core Engineering Problems

### Problem 1: Idempotency Without Distributed Locks
- **Challenge**: Same request may arrive multiple times (retries, network duplicates)
- **Risk**: Executing the transfer twice causes double debit/credit
- **Impact**: Financial data corruption, user complaints

### Problem 2: Concurrent Access to Shared State
- **Challenge**: Two transfers on the same wallet must coordinate
- **Race scenario**: 
  ```
  Wallet A: balance = $1000
  Transfer 1: debit $600
  Transfer 2: debit $600
  Both check balance, both see $1000, both execute
  Final balance: $200 (INCORRECT, should be -$200 rejected)
  ```
- **Impact**: Double-spending vulnerability

### Problem 3: Ledger Consistency Under Partial Failure
- **Challenge**: Creating transfer + ledger entries must be atomic
- **Failure mode**: Transfer created but only 1 of 2 ledger entries persisted
- **Recovery**: System is now in inconsistent state

### Problem 4: Transactional Boundaries in Multi-Step Operations
- **Challenge**: Transfer involves multiple DB operations (insert transfer, insert 2 ledger rows, update balances)
- **Which operations must be atomic?**
- **What happens if one succeeds and the next fails?**

### Problem 5: State Transition Safety
- **Challenge**: A PENDING transfer retried should not become PROCESSED twice
- **Idempotent state transitions**: Must be safe to repeat

---

## 3. Suggested Production-Grade Architecture

### 3.1 Layered Architecture

```
┌─────────────────────────────────────┐
│     HTTP Handler (Thin)             │
│  - Request validation               │
│  - Response mapping                 │
│  - Error handling                   │
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│     Service Layer (Business)        │
│  - Transfer workflow                │
│  - Idempotency orchestration        │
│  - Concurrency decisions            │
│  - Balance validation               │
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│     Repository Layer (Persistence)  │
│  - SQL queries                      │
│  - Transaction coordination         │
│  - Constraint enforcement           │
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│     Domain Models & DTOs            │
│  - Wallet, Transfer, LedgerEntry    │
│  - State enums                      │
│  - Business rules                   │
└─────────────────────────────────────┘
```

### 3.2 Directory Structure (Go)

```
wallet-transfer/
├── cmd/
│   └── server/
│       ├── main.go
│       └── config.go
│
├── internal/
│   ├── wallet/
│   │   ├── handler/          # HTTP handlers
│   │   │   ├── transfer_handler.go
│   │   │   └── balance_handler.go
│   │   │
│   │   ├── service/          # Business logic
│   │   │   ├── transfer_service.go
│   │   │   └── balance_service.go
│   │   │
│   │   ├── repository/       # Data access
│   │   │   ├── wallet_repository.go
│   │   │   └── transfer_repository.go
│   │   │
│   │   ├── domain/           # Entity models
│   │   │   ├── wallet.go
│   │   │   ├── transfer.go
│   │   │   └── state.go
│   │   │
│   │   └── dto/              # Data transfer objects
│   │       ├── transfer_request.go
│   │       └── transfer_response.go
│   │
│   ├── ledger/
│   │   ├── handler/
│   │   ├── service/
│   │   ├── repository/
│   │   ├── domain/
│   │   └── dto/
│   │
│   ├── idempotency/
│   │   ├── handler/          # Idempotency middleware
│   │   ├── service/
│   │   ├── repository/
│   │   └── domain/
│   │
│   ├── transaction/
│   │   ├── service/          # Cross-domain transaction logic
│   │   └── dto/
│   │
│   └── database/             # Shared DB layer
│       ├── connection.go
│       ├── migrations.go
│       └── schema.sql
│
├── pkg/                       # Public packages
│   ├── errors/
│   └── logger/
│
├── tests/
│   ├── unit/
│   │   ├── wallet_test.go
│   │   └── transfer_test.go
│   ├── integration/
│   │   ├── transfer_integration_test.go
│   │   └── concurrency_test.go
│   └── fixtures/
│       └── sample_data.go
│
├── docker/
│   └── docker-compose.yml
│
├── go.mod
├── go.sum
├── README.md
└── ARCHITECTURE.md
```

### 3.3 Key Design Decisions

| Component | Decision | Rationale |
|-----------|----------|-----------|
| **Transaction Strategy** | Explicit DB transactions with row-level locks | Prevents race conditions; ensures atomicity |
| **Idempotency** | Dedicated `idempotency_records` table with unique constraint | Detects duplicates; stores original response |
| **Balance Strategy** | Maintain denormalized balance + derive from ledger for verification | Fast reads; audit trail; consistency checks |
| **State Transitions** | Use database constraints (CHECK) to prevent invalid states | Enforced at DB level; application-independent |
| **Retry Logic** | Service layer orchestrates retries with exponential backoff | Handles transient failures gracefully |

---

## 4. Database Schema Design

### 4.1 Core Tables

```sql
-- Wallets
CREATE TABLE wallets (
    wallet_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    balance_cents BIGINT NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT balance_non_negative CHECK (balance_cents >= 0)
);

-- Transfers (state machine)
CREATE TABLE transfers (
    transfer_id UUID PRIMARY KEY,
    from_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    amount_cents BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' 
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT amount_positive CHECK (amount_cents > 0),
    CONSTRAINT different_wallets CHECK (from_wallet_id != to_wallet_id),
    INDEX idx_from_wallet (from_wallet_id),
    INDEX idx_to_wallet (to_wallet_id),
    INDEX idx_status (status)
);

-- Ledger Entries (double-entry bookkeeping)
CREATE TABLE ledger_entries (
    entry_id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id),
    wallet_id UUID NOT NULL REFERENCES wallets(wallet_id),
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount_cents BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    INDEX idx_transfer (transfer_id),
    INDEX idx_wallet (wallet_id)
);

-- Idempotency Records (exactly-once semantics)
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id),
    response_body JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (idempotency_key)
);
```

### 4.2 Key Constraints & Indices

| Table | Constraint | Purpose |
|-------|-----------|---------|
| `wallets` | `balance_cents >= 0` | Prevent negative balances |
| `wallets` | `currency` matches wallet type | Consistency |
| `transfers` | `amount_cents > 0` | No zero/negative amounts |
| `transfers` | `from_wallet_id != to_wallet_id` | Self-transfer prevention |
| `idempotency_records` | PRIMARY KEY on `idempotency_key` | Duplicate detection |
| `idempotency_records` | FK to `transfers` | Audit trail |
| ALL | Indices on foreign keys + status/wallet_id | Query performance |

### 4.3 Ledger Balancing Verification

**Invariant**: For every transfer, DEBIT amount = CREDIT amount

```sql
-- Verification query
SELECT 
    t.transfer_id,
    t.amount_cents,
    SUM(CASE WHEN le.entry_type = 'DEBIT' THEN le.amount_cents ELSE 0 END) as total_debit,
    SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_cents ELSE 0 END) as total_credit
FROM transfers t
LEFT JOIN ledger_entries le ON t.transfer_id = le.transfer_id
GROUP BY t.transfer_id
HAVING SUM(CASE WHEN le.entry_type = 'DEBIT' THEN le.amount_cents ELSE 0 END) != t.amount_cents
    OR SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_cents ELSE 0 END) != t.amount_cents;
```

---

## 5. Idempotency Implementation Strategy

### 5.1 The Challenge

```
Request 1: POST /transfers { idempotencyKey: "abc123", from: W1, to: W2, amount: 100 }
Response: 200 { transferId: "T1", status: "PROCESSED" }

Network failure → response lost

Request 2: Same as Request 1 (retry with same idempotencyKey)
Expected: Return same response { transferId: "T1", status: "PROCESSED" }
NOT: Create new transfer T2
```

### 5.2 Implementation Pattern

```go
// Service Layer - Idempotency Orchestration
func (s *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*TransferResponse, error) {
    
    // Step 1: Check if request already processed
    existingRecord, err := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
    if err == nil && existingRecord != nil {
        // Duplicate request - return cached response
        return existingRecord.ResponseBody, nil
    }
    
    // Step 2: Begin transaction
    tx := s.db.BeginTx(ctx, &sql.TxOptions{
        Isolation: sql.LevelSerializable,
    })
    
    // Step 3: Execute transfer atomically
    transfer, err := s.executeTransferTx(ctx, tx, req)
    if err != nil {
        tx.Rollback()
        return nil, err
    }
    
    // Step 4: Record idempotency key + response
    response := &TransferResponse{
        TransferId: transfer.ID,
        Status:     transfer.Status,
    }
    
    err = s.repo.RecordIdempotencyKey(ctx, tx, req.IdempotencyKey, response)
    if err != nil {
        tx.Rollback()
        return nil, err
    }
    
    // Step 5: Commit entire transaction atomically
    if err := tx.Commit(); err != nil {
        return nil, err
    }
    
    return response, nil
}
```

### 5.3 Key Points

| Aspect | Implementation |
|--------|----------------|
| **Deduplication** | Query `idempotency_records` table first; if hit, return cached response |
| **Atomicity** | Entire operation (transfer + idempotency record) in single transaction |
| **Response caching** | Store original response as JSONB in DB |
| **Uniqueness** | Primary key on `idempotencyKey` prevents two concurrent inserts |
| **Recovery** | After process restart, check idempotency table before re-executing |

### 5.4 Edge Cases Handled

- ✅ Duplicate requests return same response
- ✅ Idempotency persists across restarts
- ✅ Concurrent identical requests handled by DB lock
- ✅ Failed first attempt can be retried safely
- ✅ Response loss doesn't cause double-execution

---

## 6. Concurrency Handling Strategy

### 6.1 The Race Condition Problem

```
Scenario: Two transfers from same wallet (insufficient funds)

Wallet A: balance = $1000
Transfer 1: $700 from A to B
Transfer 2: $600 from A to C

Timeline:
  T1: Check balance of A → $1000 ✓ (sufficient for $700)
  T2: Check balance of A → $1000 ✓ (sufficient for $600)
  T1: Debit $700 from A → balance = $300
  T2: Debit $600 from A → balance = -$300 ✗ (INVALID!)
```

### 6.2 Serializable Transactions (Recommended for PostgreSQL)

```go
// Repository Layer - Safe Concurrent Access
func (r *WalletRepository) ExecuteTransfer(ctx context.Context, transfer *Transfer) error {
    
    // Use SERIALIZABLE isolation to prevent race conditions
    tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
        Isolation: sql.LevelSerializable,
        ReadOnly:  false,
    })
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    // Step 1: Lock source wallet row (prevents concurrent modifications)
    var currentBalance int64
    err = tx.QueryRowContext(ctx, `
        SELECT balance_cents FROM wallets 
        WHERE wallet_id = $1 
        FOR UPDATE
    `, transfer.FromWalletID).Scan(&currentBalance)
    if err != nil {
        return err
    }
    
    // Step 2: Verify sufficient funds (cannot be stale - row is locked)
    if currentBalance < transfer.AmountCents {
        return ErrInsufficientFunds
    }
    
    // Step 3: Lock destination wallet
    _, err = tx.ExecContext(ctx, `
        SELECT 1 FROM wallets 
        WHERE wallet_id = $1 
        FOR UPDATE
    `, transfer.ToWalletID)
    if err != nil {
        return err
    }
    
    // Step 4: Insert transfer record
    _, err = tx.ExecContext(ctx, `
        INSERT INTO transfers (transfer_id, from_wallet_id, to_wallet_id, amount_cents, status)
        VALUES ($1, $2, $3, $4, $5)
    `, transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.AmountCents, "PROCESSED")
    if err != nil {
        return err
    }
    
    // Step 5: Insert ledger entries (atomic with transfer)
    _, err = tx.ExecContext(ctx, `
        INSERT INTO ledger_entries (entry_id, transfer_id, wallet_id, entry_type, amount_cents)
        VALUES ($1, $2, $3, $4, $5), ($6, $2, $7, $4, $8)
    `,
        uuid.New(), transfer.ID, transfer.FromWalletID, "DEBIT", transfer.AmountCents,
        uuid.New(), transfer.ToWalletID, "CREDIT", transfer.AmountCents,
    )
    if err != nil {
        return err
    }
    
    // Step 6: Update balances
    _, err = tx.ExecContext(ctx, `
        UPDATE wallets SET balance_cents = balance_cents - $1
        WHERE wallet_id = $2
    `, transfer.AmountCents, transfer.FromWalletID)
    if err != nil {
        return err
    }
    
    _, err = tx.ExecContext(ctx, `
        UPDATE wallets SET balance_cents = balance_cents + $1
        WHERE wallet_id = $2
    `, transfer.AmountCents, transfer.ToWalletID)
    if err != nil {
        return err
    }
    
    return tx.Commit().Error
}
```

### 6.3 Locking Strategy

| Strategy | Mechanism | Pros | Cons |
|----------|-----------|------|------|
| **Row-level locks (FOR UPDATE)** | DB acquires exclusive lock on rows | Prevents race; simple | Can deadlock if lock order inconsistent |
| **SERIALIZABLE isolation** | DB serializes conflicting transactions | Complete isolation | Performance cost; may retry |
| **Optimistic locking (version column)** | App detects conflicts via version mismatch | No blocking | Requires retry logic |
| **Pessimistic locking (sequence)** | Lock all transfers in consistent order | Deadlock-free | Complex coordination |

**Recommendation**: Use **SERIALIZABLE + FOR UPDATE** with consistent lock ordering.

### 6.4 Deadlock Prevention

```go
// Always lock wallets in consistent order to prevent deadlock
func lockWalletsInOrder(tx *sql.Tx, walletA, walletB string) error {
    // Sort IDs to ensure consistent ordering
    wallets := []string{walletA, walletB}
    sort.Strings(wallets)
    
    // Lock in order: always wallet with smaller ID first
    for _, walletId := range wallets {
        _, err := tx.Exec(`
            SELECT 1 FROM wallets WHERE wallet_id = $1 FOR UPDATE
        `, walletId)
        if err != nil {
            return err
        }
    }
    return nil
}
```

---

## 7. Testing Strategy

### 7.1 Unit Tests (Domain Layer)

```go
// Test state machine rules
func TestTransferStateTransitions(t *testing.T) {
    tests := []struct {
        name      string
        fromState string
        toState   string
        valid     bool
    }{
        {"PENDING to PROCESSED", "PENDING", "PROCESSED", true},
        {"PENDING to FAILED", "PENDING", "FAILED", true},
        {"PROCESSED to FAILED", "PROCESSED", "FAILED", false},
        {"PROCESSED to PROCESSED", "PROCESSED", "PROCESSED", false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            valid := isValidTransition(tt.fromState, tt.toState)
            assert.Equal(t, tt.valid, valid)
        })
    }
}

// Test wallet balance rules
func TestWalletInsufficientFunds(t *testing.T) {
    wallet := &Wallet{Balance: 50}
    err := wallet.Debit(100)
    assert.Error(t, err)
    assert.Equal(t, wallet.Balance, 50) // Balance unchanged
}
```

### 7.2 Service Tests (Business Logic)

```go
// Test idempotency
func TestTransferIdempotency(t *testing.T) {
    service := setupTransferService(t)
    request := CreateTransferRequest{
        IdempotencyKey: "unique-123",
        FromWalletID:   "wallet_1",
        ToWalletID:     "wallet_2",
        Amount:         100,
    }
    
    // First request
    resp1, err := service.CreateTransfer(context.Background(), request)
    require.NoError(t, err)
    
    // Duplicate request
    resp2, err := service.CreateTransfer(context.Background(), request)
    require.NoError(t, err)
    
    // Must return same transfer ID
    assert.Equal(t, resp1.TransferID, resp2.TransferID)
    
    // Verify only one transfer exists in DB
    count := countTransfersInDB(t)
    assert.Equal(t, 1, count)
}

// Test ledger correctness
func TestLedgerDoubleEntry(t *testing.T) {
    service := setupTransferService(t)
    request := CreateTransferRequest{
        IdempotencyKey: "ledger-test",
        FromWalletID:   "wallet_1",
        ToWalletID:     "wallet_2",
        Amount:         100,
    }
    
    resp, err := service.CreateTransfer(context.Background(), request)
    require.NoError(t, err)
    
    // Verify 2 ledger entries exist
    entries := getTransferLedgerEntries(t, resp.TransferID)
    assert.Len(t, entries, 2)
    
    // Verify DEBIT + CREDIT balance
    var debits, credits int64
    for _, entry := range entries {
        if entry.Type == "DEBIT" {
            debits += entry.Amount
        } else {
            credits += entry.Amount
        }
    }
    assert.Equal(t, int64(100), debits)
    assert.Equal(t, int64(100), credits)
}

// Test insufficient funds
func TestInsufficientFundsRejection(t *testing.T) {
    service := setupTransferService(t)
    
    // Set wallet_1 balance to $50
    setWalletBalance(t, "wallet_1", 50)
    
    request := CreateTransferRequest{
        IdempotencyKey: "insufficient-test",
        FromWalletID:   "wallet_1",
        ToWalletID:     "wallet_2",
        Amount:         100, // More than available
    }
    
    _, err := service.CreateTransfer(context.Background(), request)
    assert.Error(t, err)
    assert.Equal(t, ErrInsufficientFunds, err)
}
```

### 7.3 Concurrency Tests

```go
// Test concurrent transfers from same wallet
func TestConcurrentTransfersFromSameWallet(t *testing.T) {
    service := setupTransferService(t)
    setWalletBalance(t, "wallet_1", 1000)
    
    numTransfers := 10
    var wg sync.WaitGroup
    
    for i := 0; i < numTransfers; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            request := CreateTransferRequest{
                IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
                FromWalletID:   "wallet_1",
                ToWalletID:     fmt.Sprintf("wallet_%d", i),
                Amount:         100,
            }
            _, err := service.CreateTransfer(context.Background(), request)
            assert.NoError(t, err)
        }(i)
    }
    
    wg.Wait()
    
    // Verify final balance is correct
    finalBalance := getWalletBalance(t, "wallet_1")
    expectedBalance := 1000 - (numTransfers * 100)
    assert.Equal(t, expectedBalance, finalBalance)
}

// Test concurrent identical requests (race for idempotency)
func TestConcurrentIdenticalRequests(t *testing.T) {
    service := setupTransferService(t)
    request := CreateTransferRequest{
        IdempotencyKey: "race-123",
        FromWalletID:   "wallet_1",
        ToWalletID:     "wallet_2",
        Amount:         100,
    }
    
    responses := make([]*TransferResponse, 5)
    var wg sync.WaitGroup
    
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            resp, err := service.CreateTransfer(context.Background(), request)
            assert.NoError(t, err)
            responses[i] = resp
        }(i)
    }
    
    wg.Wait()
    
    // All responses must be identical
    for i := 1; i < 5; i++ {
        assert.Equal(t, responses[0].TransferID, responses[i].TransferID)
    }
    
    // Only one transfer should exist
    count := countTransfersInDB(t)
    assert.Equal(t, 1, count)
}
```

### 7.4 Integration Tests

```go
// Test full workflow: create wallet, transfer, verify balance
func TestFullTransferWorkflow(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    
    service := NewTransferService(db)
    
    // Setup: Create wallets
    wallet1 := createTestWallet(t, db, "wallet_1", 500)
    wallet2 := createTestWallet(t, db, "wallet_2", 0)
    
    // Execute transfer
    resp, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
        IdempotencyKey: "workflow-test",
        FromWalletID:   wallet1.ID,
        ToWalletID:     wallet2.ID,
        Amount:         250,
    })
    require.NoError(t, err)
    assert.Equal(t, "PROCESSED", resp.Status)
    
    // Verify balances
    w1 := getWallet(t, db, wallet1.ID)
    w2 := getWallet(t, db, wallet2.ID)
    assert.Equal(t, int64(250), w1.Balance)
    assert.Equal(t, int64(250), w2.Balance)
    
    // Verify ledger
    entries := getTransferLedgerEntries(t, resp.TransferID)
    assert.Len(t, entries, 2)
}
```

### 7.5 Test Coverage Goals

| Category | Coverage | Test Cases |
|----------|----------|------------|
| **Unit** | >80% | State transitions, validation, calculations |
| **Service** | >75% | Idempotency, balance, ledger, failures |
| **Integration** | >70% | Full workflows, DB interactions |
| **Concurrency** | Behavioral | Race conditions, deadlock scenarios |

---

## 8. API Contract Suggestions

### 8.1 Request/Response Models

```yaml
# Create Transfer Endpoint
POST /transfers
Request:
  {
    "idempotencyKey": "abc-123-uuid",  # Unique identifier for this request
    "fromWalletId": "wallet_1",        # Source wallet
    "toWalletId": "wallet_2",          # Destination wallet
    "amount": 100,                     # Amount in cents
    "description": "Payment for order" # Optional
  }

Response (200 OK):
  {
    "transferId": "transfer_uuid",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100,
    "status": "PROCESSED",
    "createdAt": "2025-01-15T10:30:00Z"
  }

Response (400 Bad Request):
  {
    "error": "INVALID_AMOUNT",
    "message": "Amount must be positive"
  }

Response (402 Insufficient Funds):
  {
    "error": "INSUFFICIENT_FUNDS",
    "message": "Source wallet has insufficient balance"
  }

Response (409 Conflict):
  {
    "error": "DUPLICATE_REQUEST",
    "message": "Transfer with idempotencyKey already processed"
  }
```

### 8.2 Optional Endpoints

```yaml
# Get Transfer Status
GET /transfers/{transferId}
Response:
  {
    "transferId": "transfer_uuid",
    "status": "PROCESSED",
    "amount": 100,
    "createdAt": "2025-01-15T10:30:00Z"
  }

# Get Wallet Balance
GET /wallets/{walletId}
Response:
  {
    "walletId": "wallet_uuid",
    "balance": 1000,
    "currency": "USD",
    "lastUpdated": "2025-01-15T10:30:00Z"
  }

# List Transfer History
GET /wallets/{walletId}/transfers?limit=20&offset=0
Response:
  {
    "transfers": [
      { "transferId": "...", "amount": 100, "status": "PROCESSED" }
    ],
    "total": 150
  }
```

### 8.3 Error Codes

| Code | HTTP | Meaning |
|------|------|---------|
| `INVALID_AMOUNT` | 400 | Amount <= 0 or invalid format |
| `INVALID_WALLET` | 400 | Wallet ID not found |
| `INSUFFICIENT_FUNDS` | 402 | Source wallet has < amount |
| `SELF_TRANSFER` | 400 | from_wallet_id == to_wallet_id |
| `DUPLICATE_REQUEST` | 409 | Idempotency key already processed |
| `INTERNAL_ERROR` | 500 | Database/system error |

---

## 9. Folder Structure Recommendation

### 9.1 Rationale for Proposed Structure

```
wallet-transfer/
├── cmd/server/
│   └── main.go                 # Minimal CLI, delegates to Config + Router
│
├── internal/                   # Private to application (Go convention)
│   ├── wallet/                 # Feature domain
│   │   ├── handler/            # HTTP request/response mapping
│   │   ├── service/            # Business logic + orchestration
│   │   ├── repository/         # SQL queries + transactions
│   │   ├── domain/             # Core models (Wallet, Transfer)
│   │   └── dto/                # Transport objects
│   │
│   ├── ledger/                 # Separate domain (clear separation)
│   │   ├── handler/
│   │   ├── service/
│   │   ├── repository/
│   │   ├── domain/
│   │   └── dto/
│   │
│   ├── idempotency/            # Explicit idempotency handling
│   │   ├── service/
│   │   ├── repository/
│   │   └── domain/
│   │
│   ├── database/               # Shared infrastructure
│   │   ├── connection.go       # DB setup
│   │   ├── migrations.go       # Schema management
│   │   └── schema.sql          # DDL
│   │
│   └── transaction/            # Cross-domain coordination
│       └── service/
│
├── pkg/                        # Public/reusable (external packages)
│   ├── errors/                 # Custom error types
│   └── logger/                 # Logging utilities
│
├── tests/
│   ├── unit/                   # Unit tests (domain logic)
│   ├── integration/            # Integration tests (with DB)
│   └── fixtures/               # Test data + helpers
│
├── docker/
│   └── docker-compose.yml      # PostgreSQL + service
│
├── go.mod
├── go.sum
├── Makefile                    # Common commands
├── main.go                     # Entry point
├── README.md                   # Usage + setup
└── ARCHITECTURE.md             # Design decisions
```

### 9.2 Separation of Concerns by Layer

| Layer | Responsibility | Package | Example File |
|-------|---|---|---|
| **Handler** | HTTP semantics, request parsing, status codes | `wallet/handler/` | `transfer_handler.go` |
| **Service** | Business rules, workflows, idempotency | `wallet/service/` | `transfer_service.go` |
| **Repository** | SQL, transactions, DB access | `wallet/repository/` | `transfer_repository.go` |
| **Domain** | Models, enums, validation | `wallet/domain/` | `transfer.go` |
| **DTO** | API contracts | `wallet/dto/` | `transfer_request.go` |

### 9.3 Package Dependencies

**Allowed** ↓
```
handler → dto ✓
handler → service ✓
service → repository ✓
service → domain ✓
repository → domain ✓
```

**Forbidden** ↓
```
domain → service ✗
domain → handler ✗
repository → handler ✗
```

This ensures **domain models are independent** and can be tested in isolation.

---

## 10. What Interviewers Will Likely Evaluate

### 10.1 Core Technical Competencies

| Competency | What They're Assessing | Red Flags |
|---|---|---|
| **Database Design** | Can you design schema with integrity constraints? | No foreign keys; no CHECK constraints; inconsistent nullable fields |
| **Transaction Safety** | Do you understand isolation levels + row locking? | Race conditions in code; no explicit transactions |
| **Idempotency** | Can you implement exactly-once semantics? | No idempotency mechanism; side effects on retries |
| **Concurrency** | How do you prevent race conditions? | No locks; relies on application logic for consistency |
| **API Design** | Do you define clear contracts + error handling? | Ambiguous responses; no error codes |
| **Testing** | Do you test behavior, not implementation? | Only happy-path tests; no edge cases |

### 10.2 Code Quality Assessment

| Criterion | Strong Signal | Weak Signal |
|---|---|---|
| **Layering** | Clear separation; thin handlers | Business logic in handlers; everything in one file |
| **Naming** | `CreateTransferRequest`, `WalletRepository` | `Request`, `Repo`, `DoStuff()` |
| **Error Handling** | Custom errors + context | Generic errors; silent failures |
| **Comments** | Why decisions made | Obvious comments like `// increment counter` |
| **Consistency** | Uniform patterns across modules | Different patterns for similar things |

### 10.3 Interview Discussion Points

Interviewers will likely ask:

1. **"Why did you choose that isolation level?"**
   - Strong: "SERIALIZABLE prevents phantom reads; FOR UPDATE ensures lock ordering"
   - Weak: "It's the safest one"

2. **"What happens if the network fails after we record the transfer but before we send the response?"**
   - Strong: "Client retries; we return cached response from idempotency table"
   - Weak: "Um... the client would see an error?"

3. **"How does your solution handle concurrent transfers from the same wallet?"**
   - Strong: "Row-level FOR UPDATE locks; transactions serialize access"
   - Weak: "We check the balance before debiting"

4. **"What if there's a race between inserting the transfer and the ledger entry?"**
   - Strong: "Single transaction with both operations; DB enforces atomicity"
   - Weak: "We have error handling if the ledger insert fails"

5. **"How would you test concurrent access to the same wallet?"**
   - Strong: "Multiple goroutines concurrently submitting transfers; verify balance + ledger correctness"
   - Weak: "Our unit tests cover the logic"

6. **"Can you walk me through a failed transfer and how you recover?"**
   - Strong: Clear state machine; explain PENDING → FAILED transition; retry logic
   - Weak: "We log the error"

7. **"What constraints prevent the ledger from becoming unbalanced?"**
   - Strong: "Single transaction; FK constraints; CHECK constraints; balance verification queries"
   - Weak: "We insert both entries together"

### 10.4 Evaluation Rubric Mapping

The evaluation guide looks for:

| Rubric Item | How to Excel |
|---|---|
| **Database Schema** | Clear table design; useful indices; constraints preventing bad states |
| **Transaction Strategy** | Explicit transactions; row-level locks; serializable isolation |
| **Idempotency** | Durable storage; duplicate detection; cached response; no repeated side effects |
| **Layering** | Thin handlers; business logic in service; persistence isolated |
| **Testing** | Behavioral tests; concurrency scenarios; ledger verification |
| **Development Practices** | Clear commits; good naming; documentation in README/PR description |

---

## Summary: Production-Grade Checklist

- [ ] Database schema with FK/CHECK constraints
- [ ] Explicit transactions with appropriate isolation level
- [ ] Row-level locks (FOR UPDATE) for concurrency safety
- [ ] Idempotency table with unique constraint on key
- [ ] Service layer orchestrates entire workflow atomically
- [ ] Double-entry ledger with balancing verification
- [ ] State machine with valid transitions
- [ ] Clear error types and HTTP status codes
- [ ] Unit + integration + concurrency tests
- [ ] Locked wallet ordering prevents deadlock
- [ ] Comprehensive PR description explaining decisions
- [ ] Clean layered architecture (handler → service → repository → domain)
- [ ] All edge cases documented and tested
- [ ] Prepared to explain why each design choice was made

This comprehensive architecture demonstrates production-grade thinking and should score highly on all evaluation criteria.
