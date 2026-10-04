# AI disclosure

This note is the disclosure the assignment asks for in the pull request.

## Tool

Cursor, with the Grok 4.7 coding agent, was used to implement this submission.

## How this kind of tool is used

The usual way of working with the agent is to give it the assignment and the evaluation rubric first, require a design before code, and then review the resulting schema, transaction boundaries, and tests. The agent is useful for drafting the service, migrations, and repetitive tests. The design choices below were specified up front and checked against the running PostgreSQL tests, not accepted because the first draft compiled.

## Prompts that drove the session

The implementation session started from one instruction: read `assignment.md`, `evaluation_guide.md`, `sonar-project.properties`, and the files under `guideline/`, then implement the wallet transfer service in Go with hexagonal architecture, PostgreSQL, integer money, idempotency, concurrency controls, tests, and documentation. The agent was told not to treat the evaluation guide as optional, and to run `go test`, `go test -race`, and `go vet` before calling the work done.

Follow-up direction inside that session was to keep business rules out of handlers and repositories, to make duplicate idempotency keys return the original result, and to prove concurrent debits against a real database.

## What was checked after generation

- Domain state transitions and validation, with table-driven tests.
- Idempotent replay, payload mismatch, insufficient funds, and a missing wallet that must not consume the key.
- PostgreSQL deferred triggers that reject a committed `PENDING` transfer, an unbalanced processed transfer, and a negative balance.
- Parallel debits against one balance, parallel retries of one idempotency key, and opposite transfers that must not deadlock.
- Configuration rejects a password placed in `config.yaml`.

A full Cursor transcript can be exported from the editor session that produced this change. This file records the prompt and the review standard when that export is not attached.
