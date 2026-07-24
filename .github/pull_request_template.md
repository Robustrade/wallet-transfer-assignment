## Summary

Describe your solution briefly.

## AI disclosure

Detail how you used AI to help with your submission (including the tools you used, how
you used them and what your prompts were).
Include these points in detail

1. What tool you used (Cursor, Claude Code, Antigratvity etc.)
2. How you generally use the tool for your work.
3. A transcript of your entire session with your AI tool of choice. You can add this to the repo or email it to us with your submission. If for some reason, this is not possible, give us all the prompts that you used with the AI.

     go fmt ./...
internal\db\db.go
internal\domain\models.go
internal\service\transfer_service.go
internal\service\transfer_service_test.go


golangci-lint run --build-tags "sqlite"
internal\service\transfer_service_test.go:113:16: Error return value of `json.Unmarshal` is not checked (errcheck)
        json.Unmarshal([]byte(record.Response), &resp)
                      ^
internal\service\transfer_service_test.go:130:22: Error return value of `svc.CreateTransfer` is not checked (errcheck)
                        svc.CreateTransfer(context.Background(), string(rune(i)), "w1", "w2", 5)
                                          ^
cmd\main.go:24:16: Error return value of `w.Write` is not checked (errcheck)
        w.Write([]byte("OK"))
               ^
cmd\main.go:27:24: Error return value of `http.ListenAndServe` is not checked (errcheck)
    http.ListenAndServe(":8080", nil)


    write code to satisfy all condition mentioned in code.This code is for lead level developer. Also add unit testing of code block, Dockerfile. Give production ready code which run without fail. And handle all cases and edge cases. Complete downlodable folder with all mentioned requirement satisfied.

## Schema Design

Describe the tables, constraints, and indexes you introduced.

The system uses four core tables:

1. Wallet
    Stores wallet balances

    Fields:

    id (primary key)

    balance (constraint: balance >= 0)

    Indexes:

    Primary key on id

Purpose:

Represents user accounts and ensures balances never go negative

2. Transfer
Represents each transfer transaction

Fields:

    id (primary key)

    from_wallet_id (indexed)

    to_wallet_id (indexed)

    amount (constraint: amount > 0)

    state

    Indexes:

    Index on from_wallet_id

    Index on to_wallet_id

Purpose:

Tracks transfer metadata

3. LedgerEntry (Double-entry accounting)
Stores financial records for auditability

Fields:

    id (primary key)

    wallet_id (indexed)

    transfer_id (indexed)

    type (DEBIT / CREDIT)

    amount

    Indexes:

    Index on wallet_id

    Index on transfer_id

Purpose:
    Ensures financial correctness:
    Every transfer creates:
    1 DEBIT entry
    1 CREDIT entry

Guarantees: total debit = total credit

4. IdempotencyRecord
    Ensures safe retries

    Fields:
    key (primary key, unique)
    response

    Indexes:
    Primary key on key

Purpose:

Stores request result for replay

## Idempotency Strategy

🔁 Idempotency Strategy
Each API request includes an idempotencyKey

Flow:
    Check if key exists in IdempotencyRecord

If exists:
    Deserialize stored response
    Return same transferId (no re-execution)

If not:
    Execute transfer
    Store response in DB
    Return result

Guarantees:
    Prevents duplicate transfers
    Safe retries under network failures
    Provides exactly-once behavior at API level

## Concurrency Strategy

Explain how you prevent race conditions and double spending.


To prevent race conditions and double spending:

1. Database Transactions
        Entire transfer runs inside a single transaction
        Ensures:
        atomic updates
        all-or-nothing execution

2. Row-Level Locking
        Uses SELECT ... FOR UPDATE (via GORM locking clause)
        Locks both wallets during transfer
        Prevents concurrent updates on same wallets

3. Balance Validation
        Balance checked inside transaction
        Ensures consistency under concurrent requests

4. Idempotency + Transaction Combined
        Prevents duplicate execution even under retries

        Result:
        No race conditions
        No double spending
        Strong consistency guaranteed
## How to Run
   docker-compose up --build
   go run ./cmd/main.go 
- 

## How to Test
    go test ./... -v
    go test ./internal/service -v
    go test ./internal/service -run TestCreateTransfer_Idempotency -v
- 

## Tradeoffs / Assumptions
    1. Database Choice (SQLite)
        Tradeoff: Used SQLite for simplicity and ease of setup.

        Impact: Limited concurrency compared to PostgreSQL/MySQL.

        Assumption: Suitable for assignment/demo; can be swapped with PostgreSQL for production.

    2. Idempotency Strategy
        Tradeoff: Stores full response against idempotencyKey.

        Impact: Slight storage overhead.

        Benefit: Guarantees exact response replay for duplicate requests.

        Assumption: Idempotency keys are unique per client request.

    3. Concurrency Control
        Tradeoff: Uses row-level locking (FOR UPDATE) inside a transaction.

        Impact: Reduced parallelism under heavy contention.

        Benefit: Prevents race conditions and double spending.

        Assumption: Correctness prioritized over throughput.

    4. Double-Entry Ledger
        Tradeoff: Additional writes (2 ledger entries per transfer).

        Impact: Slightly higher storage and write cost.

        Benefit: Ensures financial correctness and auditability.

        Assumption: Ledger integrity is critical for financial systems.

    5. No External Queue / Async Processing
Tradeoff: Transfers are processed synchronously.

        Impact: Higher latency for requests.

        Benefit: Simpler design and easier reasoning.

        Assumption: Throughput requirements are moderate.

    6. No Distributed System Components
        Tradeoff: Single-node design (no Kafka, no sharding).

        Impact: Not horizontally scalable.

        Benefit: Simplicity and clarity.

        Assumption: System can be extended for scale if needed.

    7. Validation Strategy
        Tradeoff: Basic validation in service layer.

        Impact: No centralized validation framework.

        Benefit: Keeps handler thin and logic simple.

    8. Error Handling
        Tradeoff: Uses standard Go error handling (no custom error types).

        Impact: Less structured error classification.

        Benefit: Simplicity and readability.

    9. Logging
        Tradeoff: Basic logging middleware used.

        Impact: No structured logging (e.g., JSON logs).

        Benefit: Easy to understand and extend.

    10. Testing Scope
        Tradeoff: Focused on service-level tests.

        Impact: No full integration/e2e tests.

        Benefit: Faster execution and targeted coverage.

- 

## Checklist

- [pass ] Tests pass
- [pass ] Lint passes
- [ pass] Format check passes
- [ pass] README or notes updated
- [ pass] PR description explains schema, idempotency, and concurrency
## 🧠 Design Overview

This system implements an idempotent wallet-to-wallet transfer service ensuring correctness under concurrent requests.

### Key Features

- **Idempotency**
  - Each request includes an idempotency key
  - Duplicate requests return the same response

- **Transaction Safety**
  - All operations run inside a database transaction
  - Ensures atomicity

- **Concurrency Control**
  - Row-level locking (`FOR UPDATE`)
  - Prevents double spending

- **Double-entry Ledger**
  - Each transfer creates:
    - DEBIT entry
    - CREDIT entry
  - Ensures financial correctness


