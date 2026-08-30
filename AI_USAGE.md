# AI Usage

## Tool Used

ChatGPT was used as an AI development assistant during this assignment.

## How AI Was Used

AI assistance was used as a development and review aid rather than as a substitute for understanding or verification.

The main uses were:

* Understanding the assignment requirements and evaluation criteria
* Discussing PostgreSQL transaction boundaries
* Reviewing idempotency strategies
* Discussing row-level locking and concurrent transfers
* Designing behavioral tests
* Designing concurrency tests
* Diagnosing test failures
* Reviewing the implementation for correctness and maintainability
* Preparing README and PR documentation

All generated implementation changes were applied to the local repository and verified by running the application and PostgreSQL-backed test suite.

## Development Workflow

The implementation was developed incrementally.

The general workflow was:

1. Inspect the repository and assignment specification.
2. Set up PostgreSQL using Docker Compose.
3. Implement the domain, repository, service, and handler layers.
4. Run formatting and compilation checks.
5. Add behavioral tests for successful and failed transfers.
6. Add tests for persisted `FAILED` state.
7. Add concurrent debit tests to verify double-spend protection.
8. Add concurrent idempotency tests.
9. Identify and fix handling of wrapped PostgreSQL unique-constraint errors.
10. Add failed-transfer idempotency tests.
11. Add idempotency conflict tests.
12. Add concurrent reverse-transfer tests to verify deadlock avoidance.
13. Run the complete test suite.
14. Run the complete suite with the Go race detector.
15. Review the implementation against the assignment evaluation guide.
16. Prepare documentation and submission materials.

## AI Prompts / Session Record

The AI was prompted with questions and requests covering the following areas:

### Repository and environment setup

* Shared the repository structure and Go version.
* Shared Docker Compose and PostgreSQL connection output while diagnosing the local PostgreSQL/Docker port conflict.

### Database troubleshooting

* Shared Docker environment variables and PostgreSQL logs.
* Diagnosed the conflict between the local PostgreSQL installation on port 5432 and the Docker PostgreSQL instance.
* Verified the Docker database using `psql`.

### Implementation

* Discussed the repository structure and the responsibilities of the six repository files.
* Implemented service-level transfer validation, idempotency, transaction handling, wallet locking, balance updates, ledger entries, and transfer states.

### Testing

AI assistance was used to design tests for:

* Successful transfers
* Insufficient balance
* Persisting `FAILED` transfers
* Concurrent debits
* Concurrent idempotency
* Failed transfer idempotency
* Idempotency conflicts
* Concurrent reverse transfers and deadlock avoidance

### Debugging

A concurrent idempotency test initially failed with:

```text
ERROR: duplicate key value violates unique constraint
"transfers_idempotency_key_key"
(SQLSTATE 23505)
```

The issue was diagnosed as PostgreSQL's unique-constraint error being wrapped by application code. The error detection was changed to use `errors.As` to inspect the underlying `pgconn.PgError`.

The test was then rerun successfully, including repeated runs and race-detector runs.

### Verification

The following commands were used to verify the implementation:

```bash
go test ./...
```

and:

```bash
go test -race ./...
```

The complete suites passed against the PostgreSQL database.

## Developer Verification

AI-generated suggestions were not considered sufficient evidence of correctness. The implementation was executed locally, tested against PostgreSQL, and reviewed through behavioral and concurrency tests.

The developer also reviewed the implementation against the assignment and evaluation guide before preparing the submission.
