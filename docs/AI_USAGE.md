# AI Usage

## Tool Used

OpenAI Codex 

## How I Used It

I used AI as an engineering assistant during the assignment. I used it to clarify requirements, compare implementation options, reason through transaction boundaries, review idempotency and concurrency edge cases, and validate the final solution with tests and manual API checks.

I did not use AI as a one-shot generator. I worked step by step, reviewed the proposed changes, asked follow-up questions when I did not understand a design choice, and ran the application/tests locally to verify behavior.

## Planning Approach

I planned the implementation by first separating the assignment into four correctness requirements:

- atomic transfer execution,
- idempotent request handling,
- double-entry ledger consistency,
- concurrency safety.

From there, I mapped each requirement to a concrete implementation decision:

- Atomicity requires one database transaction around the full transfer workflow.
- Concurrency safety requires row-level wallet locks before reading/updating balances.
- Deadlock risk is reduced by locking wallets in deterministic wallet ID order.
- Idempotency requires a unique `idempotency_key` and a way to return the original transfer result.
- Ledger consistency requires two ledger entries to be written in the same transaction as the balance update.

I also planned the code structure before writing the full workflow:

- handlers stay thin and only handle JSON, request validation, status codes, and response mapping;
- the service owns the business workflow and transaction orchestration;
- repositories own SQL operations;
- domain objects own validation and state transitions.

I intentionally built the transaction workflow in small steps:

1. add schema for `ledger_entries` and `idempotency_records`,
2. add transaction scaffolding,
3. add transaction-scoped repository methods,
4. wire the methods in the service,
5. manually test the API,
6. add behavior and concurrency tests,
7. add read APIs and documentation.

Before implementing tests, I listed failure modes that could break correctness:

- insufficient funds after wallet lock,
- same-wallet transfers,
- duplicate idempotency requests,
- concurrent debits from the same wallet,
- two concurrent requests with the same idempotency key,
- ledger rows being duplicated or missing.

Those failure modes became the basis for the test cases.

## Prompts And Discussion Topics

These are the main prompts/topics I used during the session:

- Read the repository and summarize how the current wallet transfer project is structured.
- Review `ASSIGNMENT.md` and identify what is still missing from the implementation.
- Explain what should remain in the HTTP handler and what belongs in the service/repository layers.
- Compare two approaches for transaction handling: repository-owned transaction workflow vs service-owned transaction orchestration.
- Guide me step by step through implementing a service-owned transaction flow.
- Explain why interfaces may or may not be needed for this transaction design.
- Add transaction scaffolding with `BeginTx`, `Commit`, and `Rollback`.
- Add transaction-scoped repository methods for transfer creation, wallet locking, wallet balance updates, ledger writes, transfer status updates, and idempotency records.
- Explain why wallet locking must use `SELECT ... FOR UPDATE`.
- Explain why wallet rows should be locked in deterministic order.
- Explain how idempotency replay should work.
- Explain the race condition when two concurrent requests use the same `idempotencyKey`.
- Fix the duplicate idempotency key race so the second concurrent request returns the original transfer instead of a server error.
- Review the status code behavior for newly created transfers versus idempotent replays.
- Add service-level validation for missing idempotency keys.
- Add tests for successful transfer, ledger consistency, idempotent replay, insufficient funds, and validation failures.
- Add concurrency tests for overspend prevention and same-key idempotency races.
- Add `GET /transfers/{transferId}` and `GET /wallets/{walletId}/ledger`.
- Provide manual API test commands for creating wallets, creating transfers, replaying requests, and reading transfers/ledger entries.
- Update project README with setup, migration, API, and test instructions.
- Prepare PR notes explaining schema design, transaction strategy, idempotency strategy, concurrency handling, tests, assumptions, and tradeoffs.

## Verification Performed

I verified the implementation by:

- manually testing wallet creation, transfer creation, idempotent replay, transfer lookup, and wallet ledger lookup;
- running the normal Go test suite with `go test ./...`;
- running PostgreSQL-backed service tests with `TEST_DATABASE_URL`;
- checking that concurrency tests pass for same-wallet debits and same-key idempotency races.
