# Wallet Transfer Service — Design Proposal & Approach

## Overview
This document outlines the initial design, approach, and assumptions for implementing the Wallet Transfer Service.

The goal is to validate:
- system design decisions
- transaction strategy
- idempotency handling
- concurrency guarantees

---

## 1. Problem Understanding

We need to build a wallet-to-wallet transfer system that guarantees:

- Idempotency → same request should not execute twice
- Atomicity → debit + credit must succeed together
- Consistency → ledger must always balance
- Concurrency Safety → no double spending
- Exactly-once semantics (API level)

---

## 2. High-Level Approach

### Flow
Request → Handler → Service → DB Transaction → Response

### Key Design Decisions

- Use PostgreSQL transactions for atomicity
- Use pessimistic locking (row-level locking) (SELECT FOR UPDATE) for concurrency
- Maintain double-entry ledger in persistent storage (PostgreSQL)
- Use idempotency table with unique constraint
- Maintain wallet balance for read and write performance in PostgreSQL

---


## 3. Assumptions & Tradeoffs

- User onboarding and wallet linking are outside the scope of this system.
- Only whole-number transfers are supported; fractional amounts are not allowed.
- Idempotency key generation is handle by the client.Every transfer API call have unique idempotency key.
- Using strong consistency (DB transactions) over eventual consistency
- Single-node DB (no sharding considered) Single-node service instance.

## 4. Database Schema 

### wallets
```sql
id (PK)
balance (BIGINT NOT NULL) CHECK (balance >= 0)
created_at
updated_at
deleted_at
```

### transfers
```sql
id (PK)
from_wallet_id (FK - wallets.id)
to_wallet_id (FK - wallets.id)
amount (BIGINT NOT NULL)
state (PENDING | PROCESSED | FAILED)
idempotency_key (UNIQUE)
created_at
updated_at
deleted_at
```

### ledger_entries
```sql
id (PK)
wallet_id (FK  - wallets.id )
transfer_id (FK - transfers.id)
type (DEBIT | CREDIT)
amount
created_at
updated_at
deleted_at
```

### idempotency_records
```sql
idempotency_key (PK)
request_hash
response_payload
status_code
created_at
updated_at
deleted_at
```

---

## 5. Idempotency Strategy

- Client sends `idempotencyKey`
- Stored in:
  - transfers.idempotency_key (UNIQUE)
  - idempotency_records

### Flow POST api/v1/transfers

1. Try insert idempotency record
2. If conflict:
   - Fetch record
   - If response exists → return it
   - Else → wait / retry (another request is processing)
3. Else:
   - Execute transfer
   - Store response
   - Commit

### Guarantee
- Prevents duplicate transfers
- Ensures exactly-once API behavior

---

## 6. Transaction Strategy

Single DB transaction:

```

Create transfer table record (PENDING)

BEGIN
 
1. Lock wallets (SELECT ... FOR UPDATE)
2. Validate balance
3. Update balances
4. Insert ledger entries (DEBIT + CREDIT)
5. Update transfer → PROCESSED

COMMIT
```

### Failure Case
- Any failure → ROLLBACK
- Transfer marked as FAILED (optional retry-safe handling)

---

## 7. Concurrency Handling

### Problem
Two concurrent debits from same wallet

### Solution
- SELECT ... FOR UPDATE on wallet row
- Ensures:
  - serialized updates
  - no race conditions
  - no negative balance

---

## 8. State Machine

PENDING → PROCESSED
PENDING → FAILED

### Rules
- State transitions are idempotent
- Duplicate retries do not re-execute business logic

---

## 9. API Contract

### POST /transfers

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

### Response

```json
{
  "transferId": "T1",
  "status": "PROCESSED"
}
```

---

## 10. Failure Scenarios

Handled cases:

- duplicate request → return cached response
- insufficient balance → FAILED
- DB crash before commit → safe retry
- partial execution → prevented by transaction

---

## 11. Testing Strategy

### Unit Tests
- transfer success
- insufficient balance
- idempotency duplicate request

### Integration Tests
- DB transaction correctness
- ledger consistency

### Concurrency Tests
- parallel debit requests
- verify no double spending

---

## 12. Observability 

- structured logs (trace_id,transfer_id,idempotency_key)
- error tracking
- metrics:
  - success/failure rate
  - idempotency hits


---

## 13. Next Steps

- Implement schema migrations
- Build repository layer
- Implement service logic
- Add tests
- Add logging/metrics

---

## 14. AI Usage

- Tool: ChatGPT
- Usage:
  - brainstorming design approaches
  - validating concurrency & idempotency strategies
  - drafting PR structure

---

