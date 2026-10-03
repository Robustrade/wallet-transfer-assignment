# AI Usage Disclosure

## AI Tool Used

ChatGPT

## How AI Was Used

I used ChatGPT as a development assistant during the implementation of this
assessment.

The main areas where AI assistance was used were:

- Reviewing and breaking down the assignment requirements and evaluation guide.
- Planning the implementation and project structure before coding.
- Designing the PostgreSQL schema, constraints, and indexes.
- Discussing transaction boundaries, row-level locking, idempotency, and
  concurrency handling.
- Generating initial code scaffolding and implementation for selected
  components.
- Designing behavior-focused integration and concurrency tests.
- Investigating and fixing a PostgreSQL deadlock discovered by the concurrent
  transfer test.
- Reviewing API validation, error handling, and transaction behavior.
- Reviewing the completed implementation against the assignment requirements.
- Preparing the README and pull request documentation.

I reviewed, executed, tested, and modified the generated code throughout the
development process. The implementation was validated locally using
PostgreSQL, Docker, `go test`, `go test -race`, `go vet`, and manual API tests.

## Representative Prompts

The following are representative examples of the substantive prompts used
during development.

### Requirements and Planning

> I am working on a coding assessment provided by a company, and I have less
> than two days remaining to complete it. I am allowed to use AI during the
> development process, but I need to understand the implementation well enough
> to explain, modify, and defend it later.
>
> I have provided the repository, `ASSIGNMENT.md`, and `evaluation_guide.md`.
> Read the requirements and evaluation guidance carefully and use those
> documents as the primary source of truth.
>
> First, perform a detailed requirements analysis and identify the functional,
> database, transaction, concurrency, idempotency, API, testing, and
> evaluation requirements.
>
> Then create a detailed implementation plan for completing the assignment in
> Go using a simple database and only the additional technologies that are
> actually necessary.
>
> At this stage, do not start implementing anything. I first want to understand
> the complete scope, architecture, implementation stages, and reasoning
> behind the plan.

### Phase-by-Phase Implementation

> Based on the requirements and implementation plan we established, guide me
> through the implementation phase by phase.
>
> Assume that I have not set up anything yet. Start from the beginning,
> including Git/GitHub setup, Go module initialization, database setup, project
> structure, schema, domain models, repository, service, HTTP handler, tests,
> concurrency and idempotency testing, documentation, final validation, and
> Git/PR preparation.
>
> Work strictly one step at a time. Give me the exact commands or code needed
> for the current step, explain briefly why it is necessary, and stop so I can
> execute it and report the result before proceeding.
>
> Do not assume that I have already completed a step unless I explicitly
> confirm it.

### Concurrency and Transaction Design

> Review the assignment requirements specifically from the perspective of
> financial correctness and concurrency.
>
> Explain how the wallet transfer operation should behave when multiple
> requests arrive concurrently, including transfers involving the same wallet
> and transfers in opposite directions.
>
> Design the transaction boundary and locking strategy so that wallet balances
> cannot be double-spent, concurrent updates do not cause lost updates, the
> source balance is checked safely, wallet updates and ledger entries are
> atomic, duplicate requests remain idempotent, and deadlocks are prevented
> through consistent lock ordering.

### Debugging

> The concurrent transfer test is failing with PostgreSQL:
>
> `ERROR: deadlock detected (SQLSTATE 40P01)`
>
> Review the current transaction sequence and explain exactly why this deadlock
> can occur.
>
> Identify which database locks are involved, explain how concurrent
> transactions can end up waiting on each other, and propose the smallest
> change that fixes the problem while preserving transaction atomicity,
> idempotency, and the existing repository/service separation.

### Testing

> Based on the assignment and evaluation criteria, design a behavior-focused
> test strategy covering successful transfers, insufficient funds, idempotency,
> idempotency conflicts, database integrity, transaction rollback, concurrent
> transfers, opposite-direction transfers, and concurrent requests using the
> same idempotency key.
>
> Prioritize tests that provide strong evidence of correctness rather than
> maximizing the number of tests.

### Final Review

> Review the completed implementation against the original assignment
> requirements and evaluation guide as a final evaluator-style review.
>
> Check database constraints, transaction boundaries, wallet locking, lock
> ordering, durable idempotency, state transitions, ledger correctness, API
> validation, layering, integration tests, concurrency tests, formatting,
> static analysis, race detection, documentation, and submission readiness.
>
> Identify any remaining weaknesses and recommend only changes that materially
> improve correctness, clarity, or compliance with the assignment. Do not
> over-engineer the solution.

## Full AI Conversation

The complete ChatGPT conversation used during development is retained separately
and can be provided with the submission, as requested by the assignment.