# Wallet Transfer Service

## Overview

This project implements a wallet-to-wallet transfer service with support for:

* Idempotent transfer requests
* Double-entry ledger accounting
* Safe concurrent execution
* Transactional balance updates
* Transfer lifecycle management

The primary goal is correctness under retries, failures, and concurrent requests while maintaining ledger consistency.

---

## Architecture

The service follows a layered architecture:

```text
HTTP Handler
    ↓
Service Layer
    ↓
Repository Layer
    ↓
PostgreSQL
```

### Handler Layer

Responsible for:

* Request validation
* Transport mapping
* Response generation
* Invoking service operations

### Service Layer

Responsible for:

* Transfer orchestration
* Idempotency handling
* Balance validation
* State transitions
* Transaction boundaries

### Repository Layer

Responsible for:

* Database interaction
* Query execution
* Persistence concerns

### Domain Models

Encapsulate:

* Wallets
* Transfers
* Ledger Entries
* Transfer state transitions

---

## API

### Create Transfer

POST /transfers

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "550e8400-e29b-41d4-a716-446655440000",
  "toWalletId": "550e8400-e29b-41d4-a716-446655440001",
  "amount": 100
}
```

Behavior:

* Creates a transfer atomically
* Prevents duplicate processing using idempotency key
* Updates balances
* Creates ledger entries
* Returns existing transfer for duplicate requests

---

## Database Design

### Wallets

Stores current materialized wallet balance.

```sql
wallets
--------
id
balance
created_at
updated_at
```

### Transfers

Stores transfer metadata and lifecycle state.

```sql
transfers
---------
id
idempotency_key
from_wallet_id
to_wallet_id
amount
state
created_at
updated_at
```

### Ledger Entries

Stores immutable accounting records.

```sql
ledger_entries
--------------
id
transfer_id
wallet_id
entry_type
amount
created_at
```

---

## Double Entry Ledger

Every successful transfer generates:

Debit Entry

```text
Wallet A  -> DEBIT 100
```

Credit Entry

```text
Wallet B  -> CREDIT 100
```

Example:

```text
Transfer T1

Wallet A   DEBIT   100
Wallet B   CREDIT  100
```

This guarantees accounting consistency and provides a complete audit trail.

---

## Idempotency Strategy

The service guarantees API-level exactly-once semantics using:

```sql
UNIQUE(idempotency_key)
```

Transfer creation uses:

```sql
INSERT ... ON CONFLICT DO NOTHING
```

Behavior:

First Request

```text
Creates transfer
Processes transfer
Returns result
```

Duplicate Request

```text
Find existing transfer
Return original transfer
No duplicate side effects
```

Benefits:

* Safe client retries
* Protection from duplicate network requests
* No duplicate ledger entries
* No duplicate balance updates

---

## Concurrency Strategy

The service must prevent double spending.

Example:

```text
Wallet Balance = 100

Transfer A -> 100
Transfer B -> 100
```

Only one transfer should succeed.

To achieve this:

### Row Level Locking

Wallets are fetched using:

```sql
SELECT ...
FOR UPDATE
```

This serializes concurrent updates on the same wallet.

### Deterministic Lock Ordering

Locks are acquired in sorted wallet-id order.

This prevents deadlocks when multiple transfers operate on overlapping wallets.

Example:

```text
Always lock smaller wallet id first.
```

---

## Transaction Boundaries

The following operations occur within a single database transaction:

1. Create transfer record
2. Lock wallets
3. Validate balance
4. Update balances
5. Create debit ledger entry
6. Create credit ledger entry
7. Update transfer state

Result:

```text
All succeed
OR
All rollback
```

No partial transfers are possible.

---

## Transfer State Machine

Supported states:

```text
PENDING
PROCESSED
FAILED
```

Allowed transitions:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

Invalid transitions are rejected.

---

## Design Decisions

### Stored Balance vs Derived Balance

For simplicity and efficiency:

```text
wallet.balance
```

is stored and updated transactionally.

Alternative approach:

```text
balance =
credits - debits
```

derived from ledger entries.

This was not chosen because:

* More expensive reads
* Larger aggregation cost
* Not required for assignment scope

In production, wallet.balance could be treated as a materialized view of the ledger.

---

## Failure Handling

### Insufficient Funds

Transfer state:

```text
FAILED
```

No balance updates occur.

No ledger entries are created.

### Duplicate Requests

Existing transfer is returned.

No additional side effects occur.

### Transaction Failure

Entire transfer rolls back.

Database remains consistent.

---

## Testing

### Integration Tests

The project includes integration tests covering:

#### Successful Transfer

Validates:

* Balance updates
* Transfer creation
* Ledger generation

#### Idempotency

Validates:

* Duplicate requests
* Single transfer creation
* Single balance update

#### Concurrent Idempotency

Validates:

* Multiple concurrent requests
* Single transfer record
* Single ledger pair

#### Concurrent Transfers

Validates:

* Row locking
* Double-spend prevention
* Consistent balances

#### Insufficient Funds

Validates:

* Transfer failure
* No balance modification
* No ledger generation

---

## Testing Tradeoff

Given the limited scope and time constraints of the assignment, tests were implemented as integration tests against the database.

This provides direct validation of:

* Transaction boundaries
* Row-level locking behavior
* Concurrency guarantees
* Database constraints

In a production codebase, I would additionally introduce:

* Repository mocks
* Transaction manager abstraction
* Service-level unit tests

This would allow business logic to be tested independently of PostgreSQL while retaining a smaller set of integration tests for validating transactional behavior.

For this assignment, priority was given to verifying real database behavior for concurrency and consistency guarantees.

---

## Running

Start PostgreSQL.

Apply migrations.

Run:

```bash
go test ./...
```

Start server:

```bash
go run cmd/server/main.go
```

---

## Future Improvements

* Transaction manager abstraction
* Optimistic locking support
* Outbox pattern for event publication
* Structured logging
* OpenTelemetry tracing
* Metrics and monitoring
* Testcontainers-based integration tests
* Ledger reconciliation jobs
* Balance reconstruction from ledger
* Multi-currency wallet support

---

## Assumptions

* Single currency wallets
* Positive transfer amounts only
* Transfers between distinct wallets
* PostgreSQL is the source of truth
* Idempotency key uniquely identifies a client request
* Eventual event publication is out of scope
```
```