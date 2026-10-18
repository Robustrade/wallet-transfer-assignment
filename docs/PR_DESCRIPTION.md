## Summary

A Go (Fiber v3, GORM, PostgreSQL) service with `POST /transfers` for wallet-to-wallet transfers. Each transfer runs in **one database transaction** that claims the idempotency key, locks both wallets, moves the money, writes exactly two ledger entries (debit and credit), sets the final state and stores the response for replay. Duplicate requests return the original response, concurrent debits cannot overspend, and a failure at any step rolls everything back.

Code is organised by feature: `internal/feature/wallet/{domain,model,repository,service,handler,route}`, with the handler kept thin and the workflow in the service. Design documents are in [`docs/`](../docs).

Beyond the assignment: a one-command Docker stack (PostgreSQL, migrations, seed data, API), golangci-lint, SonarQube Cloud with coverage, and a concurrency test script.

## AI disclosure

### 1. AI tools used

- ChatGPT (OpenAI)
- Claude Code (Anthropic), model Claude Sonnet 5.5: used in this repository for review, tests, documentation, tooling and refactors (details in section 5)

### 2. How I used AI

I used AI primarily to assist with code generation and implementation.

The project structure and architecture were designed by me, based on patterns and practices I have already used in my previous projects.

Before starting the implementation, I reviewed the requirements, researched relevant technical concepts, and read several technical blogs and articles to understand different approaches, particularly around database transactions, concurrency, locking, and idempotency.

Based on my research and previous development experience, I decided on the implementation approach first and then used AI to help write the code.

My development process was:

1. **Requirement analysis:** understood the assignment requirements and identified potential challenges.
2. **Research:** read technical blogs and explored different approaches to transaction management, concurrency, and idempotency.
3. **Solution design:** defined the architecture, database structure, transaction flow, and implementation strategy.
4. **AI-assisted development:** used AI to generate code based on my defined approach and instructions.
5. **Code review:** reviewed the generated code, made necessary modifications, and checked that it followed the intended design.
6. **Testing:** tested the implementation, including concurrent transfer scenarios, and verified database consistency.

I treated AI as a coding assistant rather than relying on it to make architectural decisions or solve the assignment independently.

### 3. AI conversation transcript

I will provide the AI conversation transcript separately as part of the submission. If the complete transcript is unavailable, I will provide the prompts used during development.

### 4. Engineering ownership

The overall architecture, technical approach, and implementation decisions were based on my own experience and research.

AI helped accelerate code generation, but I remained responsible for reviewing, understanding, testing, and validating the implementation.

I am comfortable explaining the code, architectural decisions, transaction handling, concurrency strategy, and trade-offs during the PR discussion.

### 5. Use of Claude Code in this repository

Claude Code was used inside this repository, in terminal-style sessions where I gave instructions and reviewed, ran and redirected the work. It was used for:

- reviewing a failing concurrent-transfer test and tracing it to its root causes;
- writing and extending tests (unit tests with an in-memory store, PostgreSQL integration and concurrency tests);
- aligning the documents in `docs/` with the implementation;
- the Docker stack, golangci-lint and SonarQube setup, the lint and SonarQube findings, and refactors (feature-folder layout, splitting the transfer service into smaller steps).

I decided what to change and ran the commands, the race tests and the SonarQube scans myself. Problems found and fixed during these sessions, each caught by running the tests or the race script: a transfer id taken from the wrong struct variable; a deadlock caused by inserting the transfer row (and its foreign-key `FOR KEY SHARE` locks) before locking the wallets `FOR UPDATE`; `golangci-lint --fix` reordering struct fields and breaking positional struct literals in tests; a Docker healthcheck using `localhost`.

## Schema Design

Migrations are in `migrations/` (golang-migrate), tables:

- **`wallets`**: identity `id`, `balance BIGINT` with `CHECK (balance >= 0)`.
- **`transfers`**: identity `id`, unique `idempotency_key`, `from_wallet_id` / `to_wallet_id` foreign keys, `CHECK (amount > 0)`, `CHECK (from_wallet_id <> to_wallet_id)`, `status` limited to `PENDING | PROCESSED | FAILED`, optional `failure_reason`; indexes on both wallet columns.
- **`ledger_entries`**: foreign keys to `transfers` and `wallets`, `entry_type` limited to `DEBIT | CREDIT`, `CHECK (amount > 0)`, **`UNIQUE (transfer_id, entry_type)`** so a transfer can have at most one debit and one credit; index on `wallet_id`.
- **`idempotency_records`**: unique `idempotency_key` (8–64 characters), `request_hash`, nullable `transfer_id`, `response_status` and `response_body` (`JSONB`) that are either both set or both empty.

Balances are stored on `wallets` and updated under row locks. Amounts are integer minor units. See [`docs/DATABASE.md`](./DATABASE.md).

## Idempotency Strategy

- The transaction first claims the key with `INSERT … ON CONFLICT (idempotency_key) DO NOTHING`. The unique index decides the winner even when requests arrive simultaneously; the loser waits for the winner's transaction and then reads its committed result.
- A SHA-256 fingerprint of `fromWalletId`, `toWalletId` and `amount` is stored with the key. Same key and payload: the stored status and body are replayed (including a recorded `409` insufficient-funds result) with no new side effects. Same key and different payload: `409`.
- The response is saved in the same transaction as the money movement, so it exists if and only if the transfer committed. A crash before commit leaves nothing; a crash after commit (response lost) is answered by replay.
- An uncertain commit (connection lost during `COMMIT`) is resolved by looking up the saved response; only if that fails does the API return `503` and ask the client to retry with the same key.
- Replays are byte-identical to the first response: `JSONB` reformats stored JSON, so the service re-serialises it to the canonical compact form.
- A key that is claimed but has no response yet returns `409` "in progress" and is never re-executed.

## Concurrency Strategy

- `READ COMMITTED` with explicit row locks: both wallets are locked with `SELECT … FOR UPDATE` in **ascending id order** regardless of direction, so A→B and B→A cannot deadlock on each other. The balance is checked **under the lock**, so two debits cannot both spend the same funds.
- Wallets are locked **before** the `PENDING` transfer row is inserted. Inserting it first takes `FOR KEY SHARE` locks on both wallets through the foreign keys, and two concurrent transfers then deadlock when upgrading to `FOR UPDATE`. This showed up in a 10-request race test and is covered by regression tests.
- Deadlocks (`40P01`) and serialization failures (`40001`) retry the **whole transaction** (3 attempts, 100 ms then 200 ms backoff). Other errors are not retried.
- Constraints (`balance >= 0`, unique ledger pair, guarded `WHERE status = 'PENDING'` updates, foreign keys) back up the application logic.

## How to Run

- Docker (PostgreSQL, migrations, 100 seeded wallets and the API): `make docker-up`, then `curl http://localhost:8090/health`. Stop with `make docker-down`; `make docker-reset` starts from a fresh database.
- Without Docker: copy `.env.example` to `.env`, run PostgreSQL, then `make migrate-up` and `go run ./cmd/api`.
- Example request:

```bash
curl -X POST http://localhost:8090/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-key-0001","fromWalletId":1,"toWalletId":2,"amount":100}'
```

## How to Test

- `make test`: unit tests (domain, handler, route, service with an in-memory store that enforces the schema's constraints, repositories' validation, config, middleware). No database needed.
- `make test-integration`: PostgreSQL tests (build tag `integration`, database from `.env`). They create their own wallets, so existing data is untouched. They cover parallel duplicate keys, double-spend attempts, opposite-direction transfers, money conservation, ledger invariants, every repository and the transaction manager.
- `make lint`: golangci-lint v1.64.8 with `.golangci.yml`.
- `make coverage` / `make coverage-integration` (about 64% / 90%), `make sonar-scan` (SonarQube Cloud) for SonarQube.
- `MODE=opposite COUNT=100 node scripts/concurrency-test.js` (also `same-sender`, `random`, `same-key`) fires concurrent requests at a running API.

I checked that the tests catch regressions by temporarily breaking the code (dropping the idempotency conflict check, the `PENDING` guard, the lock ordering) and confirming tests fail.

## Tradeoffs / Assumptions

- Balance is a stored column updated under row locks, not derived from the ledger. Hot wallets serialise their transfers.
- Wallet and transfer ids are integers (database identity). Idempotency keys are 8–64 characters. The assignment's sample request (`"wallet_1"`, `"abc123"`) is treated as illustrative; the equivalent request here is `{"idempotencyKey":"abc12345","fromWalletId":1,"toWalletId":2,"amount":100}`.
- Insufficient funds returns `409` and records a `FAILED` transfer; a missing wallet returns `404` and rolls back, leaving the key unused. Exhausted deadlock retries return `500`.
- No authentication, so idempotency keys are global rather than per caller; keys are kept indefinitely.
- Not implemented (optional in the assignment): wallet balance and transfer history APIs, metrics, request/correlation ids, lock and statement timeouts, a database readiness check.
- **Go version:** I developed and tested everything with Go 1.26, and `go.mod` requires 1.26 because Fiber v3.6.1 and its dependencies need it. I only noticed after completing the assignment that the repository workflow pins Go 1.24 and golangci-lint v1.64.8, so the CI lint step may need a newer toolchain (Go can fetch a newer toolchain automatically when the runner allows it).
- Tests that need PostgreSQL are behind a build tag, so the CI command `go test ./...` runs only the unit tests; the integration suite is run with `make test-integration`.

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
