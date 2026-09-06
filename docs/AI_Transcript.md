

I need you to implement the Wallet Transfer Coding Assignment in the CURRENT repository from scratch.

IMPORTANT:
- First inspect the entire repository before writig code.
- Read and understand:
  - ASSIGNMENT.md
  - evaluation_guide.md
  - branch-protection-checklist.md
  - .github/copilot-instructions.md
  - .github/workflows/ci.yml
  - .github/workflows/sonarqube.yml
  - sonar-project.properties
  - README.md
  - .github/pull_request_template.md
- Treat ASSIGNMENT.md as the source of truth for functional requirements.
- Do NOT modify the assignment requirements to make implementation easier.
- Do NOT blindly generate code.
- Think through the database transaction model, concurrency model, idempotency semantics, failure modes, and testing strategy before implementation.
- Build this as a realistic production-quality backend, while keeping the scope appropriate for the assignment.
- Do not introduce unnecessary microservices or infrastructure.

==================================================
1. TECHNOLOGY STACK
==================================================

Use:

- Go
- PostgreSQL
- Gin for HTTP
- pgx/pgxpool for PostgreSQL access
- database/sql is acceptable only if there is a strong reason; prefer pgx
- golang-migrate for migrations
- testify for tests
- slog for structured logging
- Docker + Docker Compose
- Makefile for common developer commands

Use idiomatic modern Go.

Use Go modules.

Prefer standard library functionality where practical.

Do not use GORM or another heavy ORM.

The database should be the source of truth for transactional correctness.

==================================================
2. FIRST PHASE — ANALYZE BEFORE CODING
==================================================

Before implementing anything:

1. Inspect all repository files.
2. Extract every functional and non-functional requirement.
3. Identify the evaluator's likely failure cases.
4. Produce a concise implementation/design plan.
5. Design the database schema.
6. Design the transaction boundaries.
7. Design the concurrency/locking strategy.
8. Design the idempotency strategy.
9. Design the transfer state machine.
10. Design the testing strategy.

Create:

docs/design.md

It should document:

- architecture
- request flow
- database schema
- transaction boundaries
- locking strategy
- idempotency strategy
- state transitions
- failure handling
- concurrency behavior
- consistency guarantees
- API contract
- testing strategy
- observability strategy
- important tradeoffs

Do not start implementing until this design is internally consistent.

==================================================
3. ARCHITECTURE
==================================================

Use a clean layered architecture.

Suggested structure:

cmd/
  server/

internal/
  config/
  domain/
  handler/
  service/
  repository/
  database/
  middleware/
  observability/

migrations/

tests/

docs/

Keep the exact structure flexible if a better idiomatic Go structure is justified.

Responsibilities:

HTTP Handler:
- HTTP parsing
- request validation
- response mapping
- HTTP status codes
- extracting request metadata
- no business logic
- no SQL

Service:
- business rules
- transfer orchestration
- idempotency behavior
- state transitions
- validation
- transaction coordination

Repository:
- PostgreSQL operations
- queries
- row locking
- persistence
- no HTTP concerns
- no transport logic

Domain:
- wallet
- transfer
- ledger entry
- idempotency record
- transfer states
- domain validation

==================================================
4. DATABASE DESIGN
==================================================

Use PostgreSQL.

Design these core tables:

wallets
transfers
ledger_entries
idempotency_records

You may add supporting tables/columns if necessary.

Requirements:

WALLETS:
- UUID primary key
- balance
- created_at
- updated_at
- appropriate constraints
- balance must never become negative
- use NUMERIC/DECIMAL for monetary values
- NEVER use floating point for money

TRANSFERS:
- UUID primary key
- from_wallet_id
- to_wallet_id
- amount
- status
- idempotency key/reference as appropriate
- created_at
- updated_at
- processed_at if useful
- failure information if useful

Allowed states:

PENDING
PROCESSED
FAILED

Allowed transitions:

PENDING -> PROCESSED
PENDING -> FAILED

Do not allow invalid transitions.

LEDGER_ENTRIES:
- UUID primary key
- transfer_id
- wallet_id
- entry type: DEBIT/CREDIT
- amount
- created_at

Enforce referential integrity with foreign keys.

Ensure a transfer cannot accidentally have more/less than the expected ledger entries.

IDEMPOTENCY_RECORDS:
- durable idempotency key
- request fingerprint/hash
- transfer ID/result reference
- status/response information as appropriate
- created_at
- updated_at
- unique constraint on idempotency key

Add appropriate indexes.

Use database constraints wherever correctness can be enforced by the database.

Do not rely only on application-level validation.

==================================================
5. MONEY HANDLING
==================================================

Money must be represented safely.

Do NOT use:

float32
float64

Use PostgreSQL NUMERIC and an appropriate Go representation.

Define clear rules for:

- amount > 0
- maximum precision
- scale
- zero amount
- negative amount
- overflow
- invalid numeric values

Keep the representation consistent across:

HTTP
domain
service
repository
PostgreSQL

==================================================
6. CREATE TRANSFER API
==================================================

Implement:

POST /transfers

Request:

{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}

Validate:

- idempotencyKey is present
- wallet IDs are valid UUIDs
- amount is valid
- amount > 0
- source and destination wallets are different
- request is syntactically valid

Return clean, consistent JSON responses.

Use appropriate HTTP status codes.

Do not expose internal PostgreSQL errors to clients.

Define an error response format such as:

{
  "error": {
    "code": "INSUFFICIENT_FUNDS",
    "message": "insufficient wallet balance"
  }
}

Use stable application error codes.

==================================================
7. IDEMPOTENCY — VERY IMPORTANT
==================================================

This is one of the most important parts of the assignment.

Implement durable database-backed idempotency.

Requirements:

If:

Request A:
idempotencyKey = abc123

is executed successfully, then:

Request B:
idempotencyKey = abc123

must NOT execute another transfer.

It must return the original result.

Handle this correctly when:

1. Two identical requests arrive concurrently.
2. The first request commits but the HTTP response is lost.
3. The client retries after a timeout.
4. The process restarts.
5. The same key is reused with a DIFFERENT request payload.

For the same idempotency key with a different payload:

return a deterministic conflict/error such as:

IDEMPOTENCY_KEY_REUSED

Do not execute the second request.

Use a unique database constraint to protect against races.

Do not implement idempotency using an in-memory map.

Idempotency must survive process restarts.

Document the exact behavior in docs/design.md.

==================================================
8. TRANSACTIONAL TRANSFER
==================================================

The transfer must be atomic.

A successful transfer must perform all of these atomically:

1. Create/claim idempotency record.
2. Create transfer.
3. Lock source wallet.
4. Lock destination wallet.
5. Validate sufficient balance.
6. Debit source wallet.
7. Credit destination wallet.
8. Create exactly two ledger entries.
9. Change transfer state to PROCESSED.
10. Persist the result associated with the idempotency key.

If any step fails:

- rollback the entire transaction
- do not partially update balances
- do not create partial ledger entries
- do not leave an apparently successful transfer

==================================================
9. CONCURRENCY CONTROL
==================================================

This is critical.

Consider:

Wallet A balance = 100

Request 1:
A -> B = 80

Request 2:
A -> C = 80

Both arrive simultaneously.

The system MUST NOT allow both transfers to succeed.

Only one can succeed.

Use PostgreSQL row-level locking, for example SELECT ... FOR UPDATE, or another explicitly justified strategy.

Important:

Avoid deadlocks.

When transferring between two wallets:

- lock wallet rows in deterministic order based on wallet UUID
- then perform balance updates

For example:

if walletID1 < walletID2:
    lock walletID1
    lock walletID2
else:
    lock walletID2
    lock walletID1

Explain why this ordering is used.

Do not perform:

SELECT balance
then later UPDATE balance

without appropriate locking.

Avoid read-then-write races.

The database transaction must protect the entire balance mutation.

==================================================
10. DOUBLE-ENTRY LEDGER
==================================================

Every successful transfer must generate exactly two ledger entries.

Example:

Transfer T1:
A -> B
100

Ledger:

A DEBIT 100
B CREDIT 100

Requirements:

- exactly one DEBIT
- exactly one CREDIT
- both associated with the same transfer
- amounts equal
- debit wallet = source
- credit wallet = destination
- total transfer is balanced

Implement validation/invariants so a successful transfer cannot create an unbalanced ledger.

Ideally use database constraints and service-level validation where appropriate.

Add tests proving ledger correctness.

==================================================
11. BALANCE CONSISTENCY
==================================================

You may maintain a stored wallet balance.

Prefer maintaining the balance directly because the assignment explicitly allows this and it is efficient.

However, make the ledger authoritative for auditing.

Ensure:

initial_balance
+
credits
-
debits
==
wallet.balance

Document this design decision.

Consider adding tests that independently calculate balances from ledger entries and compare them with wallet balances.

==================================================
12. TRANSFER STATE MACHINE
==================================================

Create explicit domain-level transfer states:

PENDING
PROCESSED
FAILED

Allowed transitions:

PENDING -> PROCESSED
PENDING -> FAILED

Do not scatter string literals across the application.

Use typed constants/enums where appropriate.

Make state transitions safe under retries.

Do not allow:

PROCESSED -> PENDING
PROCESSED -> FAILED
FAILED -> PROCESSED

unless there is a clearly justified design reason.

The assignment only requires the transitions above.

==================================================
13. ERROR HANDLING
==================================================

Create clear domain/application errors for cases such as:

- invalid request
- wallet not found
- source wallet not found
- destination wallet not found
- same source and destination
- invalid amount
- insufficient funds
- idempotency key reused with different payload
- transfer not found
- invalid transfer state
- database failure
- unexpected internal error

Map these to proper HTTP status codes.

Do not leak:

- SQL queries
- database credentials
- stack traces
- internal implementation details

to clients.

Log internal details appropriately.

==================================================
14. HTTP API
==================================================

At minimum implement:

POST /transfers

Also implement the optional APIs because they improve the production quality of the assignment:

GET /wallets/:id/balance

GET /transfers/:id

GET /wallets/:id/transactions

If useful, add:

GET /health/live
GET /health/ready

Health endpoints must distinguish:

- process is alive
- application can communicate with PostgreSQL

Do not make liveness dependent on database availability.

==================================================
15. REQUEST IDs AND OBSERVABILITY
==================================================

Implement production-quality structured logging.

Use slog.

Generate or propagate:

X-Request-ID

Every request should have a request ID.

Log useful fields such as:

request_id
method
path
status
duration
transfer_id
wallet_id
idempotency_key (carefully, if logging it at all)
error_code

Do NOT log secrets.

Add basic metrics if reasonable:

- request count
- request latency
- transfer success count
- transfer failure count
- insufficient funds count

Do not overengineer observability.

==================================================
16. SECURITY
==================================================

Follow basic production security practices.

Implement:

- strict input validation
- request body size limits
- sensible timeouts
- graceful shutdown
- no secrets committed to git
- .env.example
- safe error responses
- SQL parameterization
- no SQL string concatenation with user input

Use configuration from environment variables.

Never hardcode:

passwords
database credentials
API keys
secrets

==================================================
17. CONFIGURATION
==================================================

Create configuration handling for:

HTTP_PORT
DATABASE_URL
LOG_LEVEL
ENVIRONMENT

Use sensible defaults for local development where appropriate.

Fail fast for required production configuration.

Provide:

.env.example

Do not commit .env.

==================================================
18. DATABASE MIGRATIONS
==================================================

Use versioned PostgreSQL migrations.

Example:

migrations/
  000001_create_wallets.up.sql
  000001_create_wallets.down.sql
  000002_create_transfers.up.sql
  000002_create_transfers.down.sql
  ...

Migrations must be:

- deterministic
- reversible where practical
- safe
- properly indexed

Do not create tables automatically from Go structs at runtime.

==================================================
19. DOCKER
==================================================

Create:

Dockerfile
docker-compose.yml

Docker Compose should provide PostgreSQL and the Go application.

The application must wait/retry appropriately for PostgreSQL readiness.

Add healthchecks.

Use a multi-stage Docker build.

The final image should be minimal.

Do not run the application as root if reasonably possible.

==================================================
20. TESTING
==================================================

Testing is a major evaluation criterion.

Write meaningful behavioral tests.

At minimum cover:

UNIT TESTS

- amount validation
- transfer state transitions
- domain validation
- error mapping where useful

INTEGRATION TESTS

Using a real PostgreSQL database:

1. successful transfer
2. insufficient funds
3. wallet not found
4. same wallet transfer
5. correct balance updates
6. exactly two ledger entries
7. debit/credit correctness
8. failed transfer rollback
9. idempotent duplicate request
10. same idempotency key with different payload
11. retry after successful commit
12. transfer state correctness

CONCURRENCY TESTS

This is extremely important.

Create tests that launch multiple concurrent transfers against the same wallet.

Example:

wallet A = 100

Run concurrently:

A -> B = 80
A -> C = 80

Verify:

- only one succeeds
- final balance is correct
- no negative balance
- no duplicate ledger entries
- no inconsistent transfer state

Also test multiple concurrent transfers where the total requested amount is within the available balance.

Test for deadlocks where practical.

Use PostgreSQL rather than mocking away the concurrency behavior.

==================================================
21. TEST DATABASE
==================================================

Make integration tests easy to run.

Prefer:

- Docker PostgreSQL
- isolated test database
- migrations executed before tests
- cleanup between tests

Do not make tests depend on a developer's manually configured local database.

Provide Makefile commands such as:

make test
make test-unit
make test-integration
make test-race
make lint
make fmt
make migrate-up
make migrate-down
make run
make docker-up
make docker-down

==================================================
22. GO QUALITY
==================================================

Follow idiomatic Go.

Run:

gofmt
go vet
go test
go test -race

Use context.Context throughout database/service calls.

Every database operation should support cancellation/timeouts.

Do not create unnecessary interfaces everywhere.

Use interfaces primarily where they provide meaningful decoupling or testability.

Avoid:

- global mutable state
- package-level database connections that cannot be controlled
- giant service functions
- giant handlers
- unnecessary abstraction
- unnecessary generics
- unnecessary dependency injection frameworks

Prefer simple, readable Go.

==================================================
23. GRACEFUL SHUTDOWN
==================================================

Implement graceful HTTP server shutdown.

On SIGINT/SIGTERM:

1. stop accepting new requests
2. allow in-flight requests to complete within a timeout
3. close database connections
4. exit cleanly

Use sensible server timeouts:

ReadTimeout
ReadHeaderTimeout
WriteTimeout
IdleTimeout

==================================================
24. API DOCUMENTATION
==================================================

Create an OpenAPI specification for the API.

Document:

- endpoints
- request schemas
- response schemas
- error responses
- status codes
- idempotency behavior

Also update README.md with:

- architecture
- prerequisites
- local setup
- Docker setup
- database setup
- migration commands
- running tests
- API examples
- concurrency strategy
- idempotency strategy
- ledger design
- design tradeoffs
- production considerations

==================================================
25. CI/CD
==================================================

Inspect the existing GitHub Actions workflows.

Make the CI actually useful for this Go project.

CI should run:

- formatting check
- go vet
- tests
- race detector where practical
- lint
- integration tests using PostgreSQL service/container

Do not blindly replace the existing CI configuration.

Adapt it to the actual project.

Ensure SonarQube configuration is compatible with the implementation.

==================================================
26. SONAR / CODE QUALITY
==================================================

Make the implementation friendly to static analysis.

Avoid:

- duplicated code
- dead code
- ignored errors
- overly complex functions
- security vulnerabilities
- magic constants
- unclear names

Keep functions reasonably small.

==================================================
27. DATABASE INTEGRITY
==================================================

Think like a financial-system engineer.

The application must remain correct even if:

- two transfers execute simultaneously
- clients retry requests
- HTTP responses are lost
- requests arrive out of order
- a process crashes
- PostgreSQL rejects an operation
- a transaction rolls back
- multiple requests target the same wallet

The database should enforce as many invariants as practical.

Explicitly identify which invariants are:

- database-enforced
- transaction-enforced
- application-enforced

Document them.

==================================================
28. FAILURE SCENARIOS
==================================================

Before considering implementation complete, reason through these scenarios:

Scenario 1:
Transfer succeeds but client never receives response.

Scenario 2:
Client retries the same idempotency key.

Scenario 3:
Same idempotency key is reused with a different amount.

Scenario 4:
Two requests attempt to spend the same balance simultaneously.

Scenario 5:
Source wallet does not exist.

Scenario 6:
Destination wallet does not exist.

Scenario 7:
Insufficient funds.

Scenario 8:
Database error occurs after wallet locking.

Scenario 9:
Database error occurs while creating ledger entries.

Scenario 10:
Application crashes after database commit but before HTTP response.

Scenario 11:
Two transfers lock wallets in opposite order.

Scenario 12:
A transfer is retried after it has already been marked PROCESSED.

For each scenario, make sure the implementation behaves correctly.

==================================================
29. CODE REVIEW PHASE
==================================================

After implementation:

DO NOT immediately stop.

Perform a senior-level code review of your own implementation.

Review specifically for:

- race conditions
- deadlocks
- idempotency bugs
- transaction boundary bugs
- rollback bugs
- double-spending
- ledger inconsistency
- incorrect HTTP semantics
- PostgreSQL constraint issues
- connection leaks
- context cancellation problems
- goroutine leaks
- error handling problems
- security problems
- test gaps
- unnecessary complexity

Then fix everything you find.

==================================================
30. TEST THE ACTUAL SYSTEM
==================================================

Run:

go test ./...
go test -race ./...
go vet ./...

Run integration tests against PostgreSQL.

Build the binary.

Build the Docker image.

Start the complete system with Docker Compose.

Test the API manually or with automated API tests.

Verify migrations from a clean database.

Verify that the application can start from scratch.

==================================================
31. FINAL VALIDATION
==================================================

Before finishing, verify every requirement from ASSIGNMENT.md.

Create:

docs/requirements-checklist.md

with a table:

Requirement | Implementation | Test | Status

Every assignment requirement must be marked as implemented and tested.

Also verify against evaluation_guide.md.

Create:

docs/evaluation-checklist.md

Cover:

- database schema
- constraints
- indexes
- transaction boundaries
- locking
- idempotency
- code structure
- testing
- development practices

==================================================
32. IMPORTANT DESIGN PREFERENCE
==================================================

Do NOT turn this into a microservices project.

One well-designed Go service with PostgreSQL is preferred.

The evaluator is assessing correctness and engineering judgment, not the number of technologies used.

Prioritize:

1. correctness
2. transaction safety
3. idempotency
4. concurrency safety
5. database integrity
6. test quality
7. clean architecture
8. observability
9. documentation
10. production readiness

over feature quantity.

==================================================
33. FINAL OUTPUT
==================================================



1. What was implemented
2. Final project structure
3. Database schema
4. Transaction strategy
5. Locking strategy
6. Idempotency strategy
7. State machine
8. Ledger design
9. Important failure scenarios handled
10. Tests added
11. Commands used to validate the project
12. Any remaining limitations
13. Files created/modified

Most importantly:

DO NOT simply tell me that the code "should work".

Actually run the tests, race detector, vet, build, migrations, and Docker setup and fix failures before declaring the implementation complete.

If something fails, diagnose the root cause and fix it.

Do not skip difficult concurrency or idempotency tests.