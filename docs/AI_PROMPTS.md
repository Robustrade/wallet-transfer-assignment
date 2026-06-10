
## Requirements Analysis
```
You are a Principal Golang Engineer and FinTech Architect.

Read the complete assignment documentation and repository structure.

Do NOT write code.

Produce:

1. Functional requirements
2. Non-functional requirements
3. Risks and edge cases
4. Concurrency concerns
5. Idempotency concerns
6. Double-entry ledger requirements
7. Architecture proposal
8. Database schema proposal
9. API contract proposal
10. Testing strategy
11. Step-by-step implementation plan

For every design choice explain tradeoffs.

Assume this will be reviewed by Staff Engineers and Engineering Managers.

Prioritize correctness, maintainability, observability, and production readiness over simplicity.
```

### Prompt 1 
```
Read ASSIGNMENT.md.

Summarize the requirements.

Identify:
- functional requirements
- non-functional requirements
- edge cases
- concurrency risks
- testing requirements

Do not write code.
```

## Architecture Discussion
### Prompt 2
```
Propose a PostgreSQL schema.

Compare:

1. Stored balance only
2. Ledger-derived balance only
3. Stored balance + ledger

Recommend one approach for this assignment.
```
### Prompt 3
```
Explain how you would implement idempotency.

Compare:

- unique constraint on transfers
- dedicated idempotency table

Discuss failure scenarios and tradeoffs.
```

### Prompt 4
```
Explain how concurrent transfers can create
double-spending problems.

Compare:

- optimistic locking
- pessimistic locking
- serializable isolation

Recommend one approach.
```

## Implementation

### Prompt 1
```
Design a production-grade architecture for this wallet transfer service.

Requirements:

- Golang
- PostgreSQL
- REST API
- Double-entry ledger
- Idempotent transfers
- Concurrent transfer safety

Provide:

1. Folder structure
2. Domain model
3. Service boundaries
4. Repository interfaces
5. Transaction boundaries
6. Error handling strategy
7. Logging strategy
8. Metrics strategy
9. Health checks
10. API versioning approach

Use clean architecture principles without overengineering.

Return architecture diagrams using markdown.
```

### Prompt 2
```
Review the proposed architecture and identify:

1. Double-spend risks
2. Race conditions
3. Deadlock scenarios
4. Lost updates
5. Replay attacks
6. Duplicate requests
7. Partial failures
8. Database failure scenarios

For each issue:

- Explain how it occurs
- Explain mitigation
- Explain test cases required

Think like a Staff Engineer reviewing a fintech payment platform.
```

### Prompt 3 - Database Design
```
Design a PostgreSQL schema for a wallet transfer platform.

Requirements:

- Wallets
- Transfers
- Ledger entries
- Idempotency records

Include:

- DDL
- Constraints
- Indexes
- Foreign keys
- Check constraints

Explain:

- Why BIGINT instead of FLOAT
- Why store balance separately
- How to prevent negative balances
- How to support auditability
- How to support future scalability

Generate migration files.
```

### Prompt 4 - Threat & Failure Analysis

```
Review the proposed architecture and identify:

1. Double-spend risks
2. Race conditions
3. Deadlock scenarios
4. Lost updates
5. Replay attacks
6. Duplicate requests
7. Partial failures
8. Database failure scenarios

For each issue:

- Explain how it occurs
- Explain mitigation
- Explain test cases required

Think like a Staff Engineer reviewing a fintech payment platform.
```

### Prompt 5 - Domain Layer
```
Implement only the domain layer.

Requirements:

- No database code
- No HTTP code

Create:

- Wallet
- Transfer
- LedgerEntry
- TransferStatus

Implement:

- Validation
- State transitions
- Business invariants

Examples:

PENDING -> PROCESSED

Allowed

PENDING -> FAILED

Allowed

PROCESSED -> FAILED

Forbidden

Generate unit tests first.

Follow TDD principles.
```

### Prompt 6 - Repository Contracts
```
Implement repository interfaces only.

Do NOT create PostgreSQL implementation yet.

Create:

WalletRepository
TransferRepository
LedgerRepository
IdempotencyRepository

Design interfaces suitable for mocking and testing.

Explain transaction ownership and repository responsibilities.
```

### Prompt 7 - PostgreSQL Repository
```
Implement PostgreSQL repositories using pgx.

Requirements:

- Context support
- Transaction support
- FOR UPDATE support
- Optimized queries
- Proper error mapping

Generate integration tests.

Assume PostgreSQL is the production datastore.
```

### Prompt 8 - Transfer Service
```
Implement TransferService.

Requirements:

1. Idempotency support
2. Transactional integrity
3. Double-entry ledger
4. Concurrent transfer safety
5. Deadlock prevention

Implementation requirements:

- Single database transaction
- Deterministic wallet locking order
- SELECT FOR UPDATE
- Ledger entries written atomically
- Idempotency record stored atomically

Provide sequence diagram.

Generate tests before implementation.
```

Prompt 9 - Concurrency Verification
```
Generate concurrency tests for TransferService.

Scenarios:

1. 100 concurrent debits from same wallet
2. Concurrent duplicate idempotency keys
3. A -> B and B -> A transfers
4. Oversubscribed wallet
5. Simultaneous credits and debits

Validate:

- No negative balances
- No duplicate transfers
- Ledger consistency
- Balance conservation

Run with race detector in mind.
```

### Prompt 10 - HTTP API
```
Implement REST API layer.

Endpoints:

POST /wallets
GET /wallets/{id}
POST /transfers
GET /transfers/{id}

Requirements:

- Request validation
- Structured errors
- OpenAPI annotations
- Context propagation
- Correlation IDs

Keep handlers thin.

Business logic must remain in services.
```

### Prompt 11 - Observability
```
Add production-grade observability.

Requirements:

- slog structured logging
- Prometheus metrics
- Request middleware
- Correlation IDs
- Transfer duration metrics
- Idempotency hit metrics
- Error metrics

Provide implementation and tests.
```

### Prompt 12 - Health Checks
```
Implement:

GET /health/live
GET /health/ready

Readiness should verify:

- PostgreSQL connectivity
- Connection pool status

Return structured JSON responses.

Generate tests.
```

### Prompt 13 - OpenAPI
```
Generate complete OpenAPI documentation.

Requirements:

- All endpoints
- Request schemas
- Response schemas
- Error responses
- Examples

Generate Swagger UI integration.
```

### Prompt 14 - Reconciliation Feature
```
Implement balance reconciliation.

Endpoint:

GET /wallets/{id}/reconcile

Response:

{
  "walletId": "...",
  "storedBalance": 1000,
  "ledgerBalance": 1000,
  "match": true
}

Validate that wallet balance equals ledger-derived balance.

Generate tests.
```

### Prompt 15 - ADR Documentation
```
Generate Architecture Decision Records.

ADR-001 PostgreSQL Selection
ADR-002 Pessimistic Locking
ADR-003 Double Entry Ledger
ADR-004 Stored Balance Strategy
ADR-005 Idempotency Design

Each ADR should include:

- Context
- Decision
- Alternatives Considered
- Consequences
```

### Prompt 16 - Technical Lead Review
```
Act as a Staff Engineer performing a final PR review.

Review:

- Architecture
- Schema
- APIs
- Concurrency handling
- Idempotency handling
- Tests
- Security
- Observability
- Documentation

Identify all weaknesses.

Suggest improvements that would differentiate this submission from other candidates.

Be extremely critical.
```
