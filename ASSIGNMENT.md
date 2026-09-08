# Wallet Transfer Service — Coding Assignment

## Overview

Build a small service that supports **wallet-to-wallet transfers**.

The goal of this assignment is to evaluate your ability to design a **reliable transactional system** with correct handling of:

- idempotency
- concurrency
- ledger consistency
- safe state transitions

This assignment reflects the design challenges commonly found in **distributed financial systems**.

Your implementation should demonstrate strong thinking about:

- database design
- transaction safety
- retry-safe behavior
- clean architecture
- testing discipline

Focus on **clarity, correctness, and robustness**, not feature completeness.

---

## Problem Statement

Implement a service that supports **wallet transfers**.

Transfers must guarantee:

- **Idempotent request handling**
- **Double-entry ledger recording**
- **Correct balance tracking**
- **Safe concurrent execution**

The API should provide **exactly-once semantics at the API level** when an `idempotencyKey` is provided.

---

## Functional Requirements

### 1. Create Transfer

Implement the endpoint:

```text
POST /transfers
```

Example request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

#### Expected Behavior

- If the same `idempotencyKey` is used again, the system must **return the original result**.
- Duplicate requests **must not trigger duplicate transfers**.
- Transfers must execute **atomically**.

### 2. Wallet Balances

The system must maintain **wallet balances**.

You may choose either:

- deriving balance from the ledger
- maintaining a stored balance updated during transfers

Your design must guarantee **correct balances under concurrent requests**.

### 3. Double-Entry Ledger

Every transfer must produce **two ledger entries**.

| entry_id | wallet_id | transfer_id | type   | amount |
|----------|-----------|-------------|--------|--------|
| 1        | wallet_1  | T1          | DEBIT  | 100    |
| 2        | wallet_2  | T1          | CREDIT | 100    |

Rules:

- every transfer must generate exactly two entries
- debit from source wallet
- credit to destination wallet
- ledger must always balance

### 4. Transfer States

Transfers should have a state machine.

Allowed states:

```text
PENDING
PROCESSED
FAILED
```

Example lifecycle:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

State transitions must be **safe under retries and duplicates**.

### 5. Concurrency Safety

Your implementation must safely handle concurrent scenarios.

Example case:

```text
Two transfers attempt to debit the same wallet simultaneously
```

The system must ensure:

- correct balances
- no double spending
- consistent ledger entries

Possible approaches include:

- database transactions
- row-level locks
- optimistic locking
- isolation levels

You should choose and justify your strategy.

### 6. Persistence

Preferred database:

```text
PostgreSQL
```

Acceptable alternative:

```text
SQLite
```

Suggested tables:

- `wallets`
- `transfers`
- `ledger_entries`
- `idempotency_records`

You may extend the schema as needed.

---

## Architecture Expectations

Your implementation should follow a **clean layered architecture**.

Example structure:

```text
handler layer
service layer
repository layer
domain models
```

Responsibilities should be clearly separated:

**Handler**

- request validation
- transport mapping
- invoking service logic

**Service**

- business logic
- orchestration
- idempotency behavior
- transfer workflow

**Repository**

- persistence operations
- database interaction

**Domain models**

- entities
- state transitions
- validation rules

Keep handlers thin and avoid mixing persistence logic with business rules.

---

## Documentation-First Workflow

Before implementing non-trivial work, define or update:

- problem statement
- expected behavior
- API or event contract
- side effects
- failure modes
- idempotency behavior
- retry behavior
- consistency expectations
- observability expectations
- testing strategy

Preferred order of work:

1. inspect current contract or spec
2. update spec or design note
3. implement code
4. add or update tests
5. verify observability and operational concerns

---

## Idempotency Requirements

Your solution must explicitly address idempotency.

You should clearly define:

- how `idempotencyKey` is stored
- how duplicate requests are detected
- how the system returns the original result
- how duplicate side effects are prevented

Possible mechanisms include:

- unique database constraints
- idempotency tables
- guarded state transitions

Explain your approach in the PR.

---

## Distributed System Thinking

Assume the system may experience:

- duplicate requests
- retries
- network failures
- partial execution

Your design should consider:

- retry-safe operations
- safe state transitions
- transactional boundaries
- failure handling

The system must behave correctly under **retries and duplicate delivery**.

---

## Testing Requirements

Testing is required.

Your solution should include tests for:

- transfer execution
- idempotency behavior
- ledger correctness
- failure scenarios

Focus on **behavioral testing**, not implementation details.

If possible, include tests demonstrating concurrency safety.

Follow this discipline:

- **Red**: write a failing test for the missing behavior
- **Blue**: implement the smallest correct solution
- **Green**: refactor safely while keeping tests passing

---

## Evaluation Criteria

Your submission will be evaluated on:

### Database Design

- schema clarity
- constraints
- data integrity
- index usage

### Transaction Strategy

- transaction boundaries
- lock strategy
- race condition handling

### Idempotency Strategy

- correctness under retries
- deduplication guarantees

### Code Structure

- clear layering
- separation of concerns
- maintainability

### Testing

- meaningful coverage
- correctness validation

---

## What We Expect

A strong submission typically includes:

- clear database schema
- safe transactional transfer logic
- explicit idempotency handling
- readable code
- thoughtful tests

The goal is not to build a full production system but to demonstrate **good engineering decisions**.

---

## Submission Instructions

1. Create a branch from `main`
2. Implement your solution
3. Add tests
4. Open a **Pull Request into `main`**

Your PR should include a short explanation of:

- database schema design
- idempotency strategy
- concurrency handling
- assumptions or tradeoffs

---

## Time Expectation

Expected effort: **3-5 hours**

Focus on correctness and clarity.

---

## AI usage

You are free to use AI tools responsibly. 

Please include a note in your PR about how you used AI. The note should include the following. Please double check and ensure that these points are all covered in the PR. 
1. What tool you used (Cursor, Claude Code, Antigratvity etc.)
2. How you generally use the tool for your work.
3. A transcript of your entire session with your AI tool of choice. You can add this to the repo or email it to us with your submission. If for some reason, this is not possible, give us all the prompts that you used with the AI.

Good use of AI to create a quality solution is a plus point. Blindly using it to solve the entire problem is not. 

We will have a short discussion on your PR and you should be able to to explain the PR and what all you did.

## Optional Enhancements

If time permits, you may also include:

- wallet balance API
- transfer history API
- observability/logging
- metrics
- retry-safe workflows

These are optional and not required for evaluation.
