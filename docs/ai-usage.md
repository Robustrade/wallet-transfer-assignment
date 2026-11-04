# AI usage disclosure

## Tool used

OpenAI ChatGPT.

## How it was used

ChatGPT was used as an interactive engineering assistant during the assignment. The work was carried out incrementally rather than by accepting a one-shot generated solution. Assistance included:

- discussing the design and documenting transaction, idempotency, concurrency, ledger, state-transition, and failure-handling decisions;
- implementing individual phases of the Go service and reviewing the separation between handler, service, repository, and domain layers;
- helping diagnose Go toolchain and PostgreSQL integration-test issues;
- analyzing test output and proposing a targeted SQL fix for PostgreSQL's ambiguous parameter types in the wallet credit query;
- reviewing API behavior, documentation accuracy, operational concerns, and assignment coverage;
- preparing and reviewing tests and documentation.

The developer reviewed the changes, pulled commits locally, configured PostgreSQL, ran the tests, and supplied the resulting output for diagnosis. The developer reported that the following commands passed on the revision before the documentation, structured-logging, and additional integration-test changes in this review:

```sh
go test -v ./internal/integration
go test ./...
go test -race -cover ./...
go vet ./...
```

The PostgreSQL integration tests passed after explicit BIGINT casts were added to the wallet credit query. The newly added structured-logging test and rollback/opposite-direction integration tests have not yet been run by the developer. Rerun the commands above after pulling the latest branch before submission. These results are developer-run local checks; this document does not claim an independent CI run.

## Prompt and transcript record

The assistance was requested through a sequence of phase-specific prompts and follow-up questions. The topics included:

1. Design the wallet transfer solution before implementation, including schema, transaction boundaries, idempotency, concurrency, and failure behavior.
2. Implement the domain models and validation rules.
3. Add PostgreSQL schema/configuration and wallet, transfer, ledger, and idempotency repositories.
4. Implement the transactional transfer service and idempotency behavior.
5. Implement the `POST /transfers` handler and its HTTP tests.
6. Wire the application to PostgreSQL, add migrations, health checks, and integration tests.
7. Diagnose the PostgreSQL integration-test failures using the supplied terminal output and fix the ambiguous arithmetic parameter types in the credit query.
8. Review the implementation against the assignment's functional, architecture, and documentation-first requirements.
9. Update the documentation and add structured transfer logging with tests.

This list is a topic-level summary, not a verbatim transcript of every prompt. The complete ChatGPT conversation export should be attached to the submission or provided separately to satisfy the assignment's request for the entire session transcript. Do not represent this summary as the complete transcript.
