# AI Usage and Development Prompts

## 1. Purpose

AI was used as an engineering assistant throughout the development of the Wallet Transfer Service:

1. Understand the requirements and correctness invariants.
2. Research and compare alternative technical approaches.
3. Select an approach based on explicit tradeoffs.
4. Create the project scaffolding.
5. Implement the system incrementally.
6. Review the implementation for correctness and race conditions.
7. Design and implement tests.
8. Verify the application locally and perform end-to-end checks.
9. Perform a final architecture and code review.

AI suggestions were treated as recommendations. Technical decisions were reviewed against the assignment requirements before being incorporated.

---

# 2. Requirement Analysis

### Prompt

> I need to implement a wallet transfer service in Go with PostgreSQL.
>
> The system must guarantee:
>
> - correct wallet balances
> - no double spending
> - consistent ledger entries
> - idempotent transfer requests
> - safe behavior under concurrent requests
>
> Before writing any code, analyze the requirements and identify the important correctness invariants we need to preserve.
>
> Don't implement anything yet. Break the problem into:
>
> 1. functional requirements
> 2. data consistency requirements
> 3. concurrency requirements
> 4. idempotency requirements
> 5. API requirements
> 6. testing requirements
>
> Explain what could go wrong if each requirement is implemented incorrectly.

### Purpose

This was used to understand the problem before making implementation decisions and to identify the invariants that the implementation and tests must preserve.

---

# 3. Researching Concurrency Approaches

### Prompt

> For the wallet transfer problem, compare the following approaches for preventing double spending:
>
> 1. PostgreSQL row-level locking using SELECT FOR UPDATE
> 2. optimistic locking with a version column
> 3. SERIALIZABLE transaction isolation
> 4. application-level mutexes
>
> For each approach explain:
>
> - how it works
> - correctness guarantees
> - behavior under concurrent transfers
> - deadlock/conflict behavior
> - retry requirements
> - performance characteristics
> - scalability implications
> - implementation complexity
>
> Then recommend an approach for a Go + PostgreSQL wallet service and explain why.
>
> Do not simply recommend the most obvious option. Explicitly discuss the tradeoffs.

### Follow-up: Challenge the decision

> We are considering PostgreSQL row-level locking with SELECT FOR UPDATE.
>
> Act as a skeptical senior backend engineer and challenge this design.
>
> Identify:
>
> - possible race conditions
> - possible deadlocks
> - transaction boundary mistakes
> - failure scenarios
> - situations where row locking would not be sufficient
>
> Also explain how deterministic lock ordering can reduce deadlocks when a transfer touches two wallets.
>
> At the end, state whether you would still choose row-level locking and why.

### Decision

The selected approach was PostgreSQL transactions combined with row-level wallet locking and deterministic lock ordering.

The reasoning was:

- The wallet row is the resource being concurrently modified.
- `SELECT ... FOR UPDATE` directly serializes competing modifications to that wallet.
- The balance can be checked while the lock is held.
- PostgreSQL handles waiting between concurrent transactions.
- Deterministic ordering of the two wallet locks reduces deadlock risk.
- The approach is simpler to reason about for this assignment than optimistic locking with application retries.
- `SERIALIZABLE` was considered but would introduce serialization failures and retry handling that are not necessary when the critical wallet rows are explicitly locked.
- Application-level mutexes were rejected because correctness must be maintained across processes/instances, not only within one Go process.

---

# 4. Researching Idempotency Approaches

### Prompt

> Design an idempotency strategy for a money transfer API.
>
> Compare:
>
> - idempotency key stored in PostgreSQL
> - unique constraint on the key
> - application-level cache such as Redis
> - request fingerprint/hash
> - storing the complete response
>
> Explain the failure scenarios around:
>
> - client retries
> - server timeout after commit
> - two identical requests arriving simultaneously
> - same idempotency key with different request parameters
>
> Recommend a design for this assignment using PostgreSQL and explain the tradeoffs.

### Decision

A PostgreSQL-backed idempotency record was selected.

The design uses:

- an idempotency key
- a deterministic request hash
- a database uniqueness constraint
- association between the idempotency record and the transfer

This means:

- the same key + same request returns the original transfer
- the same key + different request is rejected
- concurrent requests cannot both create independent idempotency records

---

# 5. Database Schema Design

### Prompt

> Propose a PostgreSQL schema for a wallet transfer system.
>
> We need:
>
> - wallets
> - transfers
> - ledger entries
> - idempotency records
>
> The system must maintain correct balances and provide an auditable ledger.
>
> Design the tables, relationships, primary keys, foreign keys, indexes, unique constraints and CHECK constraints.
>
> Explain why each constraint exists and which application bug or race condition it protects against.
>
> Also discuss whether money should be stored as NUMERIC, FLOAT, or integer minor units, and recommend one.

### Decision

The schema uses four primary tables:

- `wallets`
- `transfers`
- `ledger_entries`
- `idempotency_records`

Money is stored as an integer representing the smallest currency unit. For INR, for example:

```text
₹100.00 = 10000 paise
```

This avoids floating-point rounding issues.

A successful transfer creates:

```text
DEBIT  source wallet       amount
CREDIT destination wallet  amount
```

Both ledger entries are associated with the same transfer.

---

# 6. Transaction Workflow

### Prompt

> Based on the selected PostgreSQL row-locking and idempotency strategy, design the exact transaction workflow for:
>
> POST /transfers
>
> Show the sequence of database operations from BEGIN to COMMIT.
>
> Include:
>
> - idempotency lookup
> - transfer creation
> - wallet locking
> - balance validation
> - balance updates
> - ledger entries
> - transfer status update
>
> For every step explain what happens if it fails.
>
> Also explain which operations must be inside the same transaction and why.

### Resulting workflow

```text
BEGIN
  |
  |-- idempotency handling
  |
  |-- create PENDING transfer
  |
  |-- lock source/destination wallets
  |
  |-- check source balance
  |
  |-- debit source
  |
  |-- credit destination
  |
  |-- create DEBIT ledger entry
  |
  |-- create CREDIT ledger entry
  |
  |-- PENDING -> PROCESSED
  |
COMMIT
```

If an unexpected database failure occurs before commit, the transaction is rolled back so that partial financial state cannot remain.

---

# 7. Go Project Scaffolding

### Prompt

> We have now selected:
>
> - Go
> - PostgreSQL
> - repository/service/handler layering
> - PostgreSQL transactions
> - SELECT FOR UPDATE for wallet locking
> - deterministic wallet lock ordering
> - PostgreSQL uniqueness for idempotency keys
> - integer minor units for money
>
> Create only the initial project scaffolding.
>
> Do not implement the transfer business logic yet.
>
> Create:
>
> - Go module
> - cmd/server
> - internal/domain
> - internal/service
> - internal/repository
> - internal/handler
> - internal/database
> - migrations
> - Docker Compose for PostgreSQL
> - README
>
> Keep the code minimal and compilable. Explain the responsibility of each package.

### Purpose

The goal was to establish a clean project structure before implementing business logic, rather than generating the entire application in one step.

---

# 8. Reviewing the Scaffold

### Prompt

> Review the current Go project structure as a senior Go engineer.
>
> Do not rewrite the project.
>
> Check:
>
> - package responsibilities
> - dependency direction
> - whether the service layer contains business logic
> - whether repositories are database-specific
> - transaction handling
> - testability
> - error handling
>
> Identify problems and recommend specific improvements before we implement the transfer logic.

### Purpose

This was used as a design review before implementation.

---

# 9. Implementing the Repository Layer

### Prompt

> Implement the PostgreSQL repository layer incrementally.
>
> Create repository interfaces and PostgreSQL implementations for:
>
> - wallets
> - transfers
> - ledger entries
> - idempotency records
>
> The repositories must support both a connection pool and an active transaction.
>
> Introduce a DBTX abstraction where appropriate so repository methods can operate against pgxpool.Pool or pgx.Tx.
>
> Do not put business rules into repositories.
>
> Explain the transaction implications of each method, especially GetWalletForUpdate.

### Important implementation decision

A `DBTX` abstraction allows repository methods to work with either:

```text
pgxpool.Pool
```

or:

```text
pgx.Tx
```

This allows the service to execute all transfer-related operations using the same transaction.

---

# 10. Implementing the Transfer Service

### Prompt

> Implement the transfer service using the previously agreed design.
>
> The entire transfer operation must execute inside one PostgreSQL transaction.
>
> Required flow:
>
> BEGIN
> → idempotency handling
> → create PENDING transfer
> → acquire source/destination wallet locks in deterministic order
> → verify sufficient balance
> → debit source
> → credit destination
> → create DEBIT ledger entry
> → create CREDIT ledger entry
> → mark transfer PROCESSED
> → COMMIT
>
> For insufficient funds, persist the transfer as FAILED without modifying wallet balances.
>
> Ensure rollback occurs for unexpected database errors.
>
> Don't introduce new architectural patterns unless there is a concrete reason.

---

# 11. Code Review of the Transfer Implementation

### Prompt

> Review the transfer service implementation as if you were performing a production code review.
>
> Focus specifically on:
>
> 1. race conditions
> 2. double spending
> 3. idempotency races
> 4. transaction boundaries
> 5. deadlocks
> 6. rollback behavior
> 7. ledger consistency
> 8. error handling
>
> Don't rewrite the code immediately.
>
> First identify every potential issue and explain whether it is:
>
> - a correctness bug
> - a performance concern
> - a maintainability concern
> - or not actually a problem.
>
> Only after identifying the issues, propose fixes.

### Purpose

The AI was used here as a reviewer rather than simply as a code generator. The implementation was checked against concrete failure scenarios and invariants.

---

# 12. API Implementation

### Prompt

> Implement the HTTP API layer for POST /transfers.
>
> The handler should:
>
> - decode JSON
> - reject malformed JSON
> - validate required fields
> - validate UUIDs
> - validate positive amounts
> - call the service layer
> - map domain errors to appropriate HTTP status codes
> - return a stable JSON response
>
> Keep business logic out of the HTTP handler.
>
> Also explain the error mapping you choose.

---

# 13. Test Strategy Before Writing Tests

### Prompt

> Don't write tests yet.
>
> Design a comprehensive test strategy for this wallet transfer service.
>
> Categorize tests into:
>
> - unit tests
> - repository/integration tests
> - API tests
> - concurrency tests
> - idempotency tests
> - failure/rollback tests
>
> For every test explain:
>
> - setup
> - action
> - expected result
> - invariant being verified
>
> Pay particular attention to tests that can distinguish a truly concurrency-safe implementation from one that only works sequentially.

### Purpose

Tests were derived from the correctness requirements instead of only testing the happy path.

---

# 14. Implementing Normal Integration Tests

### Prompt

> Implement integration tests for:
>
> 1. successful transfer
> 2. insufficient funds
> 3. nonexistent wallet
> 4. successful ledger creation
> 5. exactly one debit and one credit
> 6. idempotent retry
> 7. same idempotency key with different payload
> 8. transaction rollback
>
> Use PostgreSQL rather than mocking the database for tests that validate transaction and locking behavior.
>
> Make the tests verify database state rather than only checking HTTP responses.

---

# 15. Concurrency Tests

### Prompt

> Now implement concurrency tests for the wallet transfer service.
>
> Test scenario 1:
>
> Wallet A has 10000 units.
>
> Concurrently execute:
>
> A → B = 10000
> A → C = 10000
>
> Expected:
>
> - exactly one transfer succeeds
> - exactly one fails due to insufficient funds
> - A ends at 0
> - B + C ends at 10000
>
> Test scenario 2:
>
> Send 10 concurrent requests with the same idempotency key and identical payload.
>
> Expected:
>
> - all callers receive the same transfer ID
> - only one balance change occurs
> - exactly two ledger entries exist
>
> Make the tests actually execute concurrently using goroutines and synchronization rather than simply running requests sequentially.

### Why these tests matter

The first test validates the central **no-double-spending** invariant.

The second validates that the idempotency strategy remains safe even when duplicate requests arrive at the same time.

---

# 16. Reviewing the Tests

### Prompt

> Review these tests as a skeptical reviewer.
>
> Determine whether they could pass even if the implementation had a race condition.
>
> Look for:
>
> - tests that are accidentally sequential
> - insufficient concurrency
> - missing database assertions
> - missing rollback verification
> - incorrect assumptions about transaction timing
> - tests that don't verify ledger consistency
>
> Suggest stronger tests where necessary.

### Purpose

The purpose was to avoid writing tests that merely provide coverage without actually validating concurrency correctness.

---

# 17. Running and Troubleshooting

### Prompt

> I have now implemented the application and tests.
>
> I will provide the output from:
>
> go test ./...
>
> and, if applicable:
>
> go test -race ./...
>
> Analyze the actual errors rather than assuming the cause.
>
> For each failure:
>
> 1. identify the root cause
> 2. explain why it occurs
> 3. propose the smallest appropriate fix
> 4. explain whether the fix changes the architecture or only corrects an implementation issue
>
> Don't make unrelated changes.

### Purpose

AI was used to interpret actual compiler/test failures rather than asking it to invent expected results.

---

# 18. Manual End-to-End Verification

### Prompt

> The automated tests are passing.
>
> Now help me perform manual end-to-end verification of the running application.
>
> Provide commands to:
>
> 1. start PostgreSQL
> 2. run migrations
> 3. insert test wallets
> 4. start the Go API
> 5. perform a successful transfer
> 6. inspect wallet balances
> 7. inspect ledger entries
> 8. repeat the same idempotency request
> 9. test an idempotency conflict
> 10. test insufficient funds
>
> For each step explain what result I should verify rather than simply giving commands.

### Verification

The application was then run locally and the database/API behavior was checked directly.

Important checks included:

- wallet balances before and after a transfer
- exactly two ledger entries
- retry returning the original transfer
- no second balance modification on retry
- idempotency conflict for a different payload
- insufficient funds not changing balances
- concurrent transfers not overspending the source wallet

---

# 19. Final Architecture Review

### Prompt

> Perform a final architecture and correctness review of the completed wallet transfer service.
>
> Review the implementation against these invariants:
>
> - balances are correct
> - no double spending
> - successful transfers have exactly two balanced ledger entries
> - retries are idempotent
> - concurrent identical requests don't duplicate transfers
> - concurrent spending cannot overspend a wallet
> - failed transfers don't modify balances
> - transaction rollback prevents partial state
>
> Also identify limitations and tradeoffs that should be documented in the assignment.
>
> Do not recommend unnecessary complexity. Distinguish between requirements that are satisfied, assumptions made, and future improvements.

---

# 20. Final Code Review

### Prompt

> Treat the current repository as a code submission for a senior backend engineering assignment.
>
> Review:
>
> - Go idioms
> - error handling
> - context propagation
> - transaction handling
> - PostgreSQL usage
> - SQL safety
> - concurrency
> - idempotency
> - database constraints
> - test quality
> - project structure
> - README/documentation
>
> Rank findings as:
>
> CRITICAL / HIGH / MEDIUM / LOW.
>
> Only classify something as a correctness issue if you can explain a concrete scenario in which the implementation violates a required invariant.

---

# 21. Responsible AI Usage Summary

AI was used as an engineering assistant rather than as a replacement for engineering judgment.

The workflow was:

```text
Requirements
    ↓
Identify correctness invariants
    ↓
Research alternative approaches
    ↓
Compare tradeoffs
    ↓
Select architecture
    ↓
Create scaffolding
    ↓
Implement incrementally
    ↓
Review implementation
    ↓
Design tests around invariants
    ↓
Run tests
    ↓
Troubleshoot actual failures
    ↓
Run application manually
    ↓
Verify database/API behavior
    ↓
Perform final review
```

The most important design decisions were reviewed before implementation. In particular, concurrency control and idempotency were considered as alternative design problems rather than being accepted as automatically generated implementation details.

AI-generated suggestions were reviewed and adapted to the actual project requirements. Code was executed and tested locally rather than being considered correct solely because it was generated successfully.

---

# 22. Key Engineering Decisions

### Concurrency

Selected:

```text
PostgreSQL transaction
        +
SELECT FOR UPDATE
        +
deterministic lock ordering
```

instead of relying on in-process synchronization or optimistic locking.

### Idempotency

Selected:

```text
idempotency key
        +
request hash
        +
PostgreSQL unique constraint
        +
same transaction as transfer
```

### Financial correctness

Selected:

```text
integer minor currency units
        +
stored wallet balance
        +
double-entry ledger
        +
atomic database transaction
```

### Testing

Focused particularly on:

```text
normal transfer
insufficient funds
idempotency
idempotency conflict
rollback
concurrent spending
concurrent duplicate requests
ledger consistency
```

These tests directly correspond to the business invariants required by the assignment.
