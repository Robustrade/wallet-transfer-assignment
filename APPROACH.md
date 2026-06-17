  # Wallet Transfer Service - Approach

  ## Purpose of This First Change

  This file is the documentation-first / dummy PR artifact requested by the
  assignment. It captures my understanding, implementation plan, and review
  strategy before adding the service code.

  The goal is to make the eventual implementation easy to review: first explain the
  correctness model, then implement it in small commits that map directly to this
  plan.

  ## Understanding

The service must support wallet-to-wallet transfers with API-level exactly-once
behavior. `POST /transfers` requires an `idempotencyKey`; a retry with the same
key must return the original result and must not create another transfer, mutate
wallet balances again, or add duplicate ledger entries.

  Correctness is the main evaluation target. The implementation should prove that:

  - transfers are atomic
  - wallet balances remain correct under concurrent debits
  - every processed transfer has exactly one debit and one credit ledger row
  - ledger entries balance for each processed transfer
  - transfer state transitions are limited to `PENDING -> PROCESSED` and
    `PENDING -> FAILED`
  - idempotency survives process restarts because it is stored durably
  - duplicate requests and client retries are safe

  ## Proposed Java Stack

  - Language: Java
  - Framework: Spring Boot
  - Build tool: Maven
  - Database: PostgreSQL
  - Persistence: Spring JDBC / `JdbcTemplate`
  - Migrations: Flyway
  - Tests: JUnit 5, AssertJ, Testcontainers for PostgreSQL-backed integration tests

  I plan to use Spring JDBC instead of a heavier ORM because the assignment is
  centered on transaction boundaries, row locks, constraints, and explicit SQL.
  That keeps the critical persistence behavior visible to the reviewer.

  ## API Contract

  ### `POST /transfers`

  Request:

  ```json
  {
    "idempotencyKey": "abc123",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }
  ```

  Successful response:

  ```json
  {
    "transferId": "transfer_...",
    "state": "PROCESSED",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }
  ```

  Expected error cases:

- `400 Bad Request` for invalid JSON, missing `idempotencyKey`, missing wallet
  ids, same source and destination wallet, or non-positive amount
  - `404 Not Found` if either wallet does not exist
  - `409 Conflict` for insufficient funds
  - `422 Unprocessable Entity` if an existing idempotency key is reused with a
    different request payload
  - `500 Internal Server Error` for unexpected failures

  Optional if time permits:

  - `GET /wallets/{id}` for current balance
  - `GET /transfers/{id}` for transfer state and ledger rows

  ## Data Model

  ### `wallets`

  - `id varchar primary key`
  - `balance bigint not null check (balance >= 0)`
  - `created_at timestamptz not null`
  - `updated_at timestamptz not null`

  Balances are stored and updated transactionally. This makes concurrency behavior
  explicit and efficient. The ledger remains the audit trail.

  ### `transfers`

  - `id uuid primary key`
  - `idempotency_key varchar not null unique`
  - `request_hash varchar not null`
  - `from_wallet_id varchar not null references wallets(id)`
  - `to_wallet_id varchar not null references wallets(id)`
  - `amount bigint not null check (amount > 0)`
  - `state varchar not null check (state in ('PENDING', 'PROCESSED', 'FAILED'))`
  - `failure_reason varchar null`
  - `created_at timestamptz not null`
  - `updated_at timestamptz not null`

  Indexes:

  - unique index on `idempotency_key`
  - index on `from_wallet_id`
  - index on `to_wallet_id`

  ### `ledger_entries`

  - `id bigserial primary key`
  - `transfer_id uuid not null references transfers(id)`
  - `wallet_id varchar not null references wallets(id)`
  - `entry_type varchar not null check (entry_type in ('DEBIT', 'CREDIT'))`
  - `amount bigint not null check (amount > 0)`
  - `created_at timestamptz not null`

  Constraints and indexes:

  - unique constraint on `(transfer_id, entry_type)`
  - index on `(wallet_id, created_at)`

  The `(transfer_id, entry_type)` constraint prevents duplicate debit or credit
  rows for a transfer. The service will create both rows in the same database
  transaction that updates wallet balances and marks the transfer `PROCESSED`.

## Idempotency Strategy

The idempotency key is required for every transfer request and is stored on
`transfers` with a non-null unique database constraint. The normalized request is
hashed and stored as `request_hash`.

The request hash is calculated from the transfer intent, not from the raw JSON
body. This avoids false mismatches caused by JSON field order, whitespace, or
formatting differences.

Canonical hash input:

```text
fromWalletId=<fromWalletId>|toWalletId=<toWalletId>|amount=<amount>
```

Rules:

- include `fromWalletId`, `toWalletId`, and `amount`
- exclude `idempotencyKey` because it is the lookup key, not part of the transfer
  intent
- trim wallet ids during validation before hashing
- represent `amount` as a base-10 integer minor-unit string
- hash the canonical string with SHA-256
- store the lowercase hex-encoded digest in `request_hash`

Flow:

  1. Begin one database transaction.
  2. Try to insert a `PENDING` transfer with the idempotency key and request hash.
  3. If the insert conflicts on `idempotency_key`, load the existing transfer.
  4. If the existing request hash differs, return `422`.
  5. If the existing request hash matches, return the existing transfer result
    without replaying side effects.
  6. If this is a new transfer, perform the balance update, ledger insert, and
    state transition inside the same transaction.

  This handles the important retry case where the first request commits but the
  client never receives the response.

  ## Transaction and Concurrency Strategy

  All transfer side effects happen in one PostgreSQL transaction managed by
  Spring's `@Transactional` boundary in the service layer.

  For a new transfer:

  1. Insert the `PENDING` transfer row.
  2. Lock both wallet rows using `SELECT ... FOR UPDATE`.
  3. Acquire locks in deterministic wallet id order to reduce deadlock risk.
  4. Validate source balance after locks are acquired.
  5. If funds are insufficient, mark transfer `FAILED` and commit that final state.
  6. If funds are sufficient, decrement the source balance and increment the
    destination balance.
  7. Insert exactly one debit ledger entry and one credit ledger entry.
  8. Mark transfer `PROCESSED`.
  9. Commit.

  The key correctness point is that balance validation and balance updates occur
  while the wallet rows are locked, so concurrent debits from the same wallet
  serialize and cannot both spend the same funds.

  ## Layering

  Planned package shape:

  ```text
  src/main/java/.../wallettransfer
    api
    application
    domain
    persistence
    config

  src/main/resources/db/migration
  src/test/java/.../wallettransfer
  ```

  Responsibilities:

  - `api`: controllers, request/response DTOs, exception-to-HTTP mapping
  - `application`: transfer use case, idempotency workflow, transaction boundary
  - `domain`: transfer states, ledger entry types, validation, domain errors
  - `persistence`: SQL repositories and transaction-scoped database operations
  - `config`: database, clock, and application wiring

  Handlers will stay thin. Business decisions such as insufficient funds,
  idempotency replay, and state transitions belong in the application/domain
  layers. SQL belongs in repository classes.

  ## Testing Plan

  Behavioral tests will cover:

  - successful transfer updates both wallet balances
  - successful transfer creates exactly two ledger entries
  - ledger entries for a processed transfer net to zero
  - duplicate idempotency key returns the original transfer
  - duplicate idempotency key does not create extra ledger entries
  - same idempotency key with a different payload is rejected
  - insufficient funds produces a failed transfer without ledger rows
  - invalid amount and same-wallet transfer are rejected
  - concurrent debits from the same wallet cannot overspend

  The concurrency test will run multiple requests against the real service and a
  real PostgreSQL database via Testcontainers. The assertion will verify that the
  number of processed transfers and final source balance are consistent with the
  initial available funds.

  ## Review-Friendly Commit Plan

  1. First PR / first commit: add this `APPROACH.md` only.
  2. Commit 2: scaffold Java Spring Boot project, Maven build, formatting, and a
    minimal health endpoint.
  3. Commit 3: add Flyway migrations for wallets, transfers, and ledger entries.
  4. Commit 4: add domain model, errors, and validation.
  5. Commit 5: add repository layer and transaction-safe SQL operations.
  6. Commit 6: implement transfer service, idempotency, and state transitions.
  7. Commit 7: add HTTP API and exception mapping.
  8. Commit 8: add integration tests for success, idempotency, failure, ledger
    correctness, and concurrency.
  9. Commit 9: update README, run instructions, test instructions, PR notes, and AI
    disclosure.

  This order lets the reviewer inspect the design, schema, core workflow, API, and
  tests separately.

  ## Two Working Day Execution Plan

  Assuming today is Tuesday, June 16, 2026, the target deadline is Thursday,
  June 18, 2026.

  ### Day 1

  - Open the approach-only PR or commit.
  - Scaffold the Java Spring Boot project.
  - Add Maven dependencies and formatting/test commands.
  - Add Flyway migrations and seed/test fixture support.
  - Implement repository transaction primitives.
  - Implement the core transfer service.
  - Add tests for happy path, idempotency replay, payload mismatch, and
    insufficient funds.

  ### Day 2

  - Add concurrency test coverage using Testcontainers.
  - Add HTTP controllers and request/response mapping.
  - Add optional balance lookup only if the core transfer behavior is complete.
  - Tighten domain errors, validation, README, and PR explanation.
  - Run format, tests, and any configured CI checks.
  - Prepare the final PR description with schema, idempotency, concurrency,
    assumptions, tradeoffs, and AI disclosure.

  ## CI Plan

  The current repository CI is language-agnostic in concept but contains Go setup
  steps from the template. For the Java solution, I will update it to:

  - set up JDK 21
  - cache Maven dependencies
  - run `mvn -B test`
  - run formatting or style checks if added

  Suggested repository variables after the Java scaffold exists:

  - `LINT_CMD=./mvnw -B checkstyle:check` if Checkstyle is added
  - `FORMAT_CHECK_CMD=./mvnw -B spotless:check` if Spotless is added
  - `TEST_CMD=./mvnw -B test`

  If keeping the workflow simple is preferred, the CI can directly run
  `./mvnw -B test` without repository command variables.

  ## PR Description Outline

  The final PR should be structured to match the repository template:

  - Summary: concise explanation of the transfer service and core guarantees
  - AI disclosure: tool used, how it was used, and prompt/session transcript note
  - Schema design: tables, constraints, indexes, and why stored balances were used
  - Idempotency strategy: unique key, request hash, replay behavior, mismatch error
  - Concurrency strategy: single transaction, row-level wallet locks, lock ordering
  - How to run: local app and PostgreSQL/Testcontainers notes
  - How to test: Maven test command
  - Tradeoffs / assumptions: integer money, PostgreSQL-first, wallet creation scope

  ## Tradeoffs and Assumptions

  - Amounts are integer minor units to avoid floating point money errors.
  - Wallet creation is not the focus of the assignment, so test fixtures or seed
    data are acceptable.
  - Stored balances are used for clarity and efficient locking; ledger entries
    remain the audit trail.
  - Failed transfers are stored to preserve idempotent replay behavior for requests
    that were valid but could not be processed, such as insufficient funds.
  - PostgreSQL-specific row locking is acceptable because PostgreSQL is the
    preferred database in the assignment.
  - Spring JDBC is chosen to keep transaction and locking behavior explicit.

  ## Day 2 Execution Notes

  Day 2 completed on Tuesday, June 16, 2026.

  ### What was implemented

  #### 1. `pom.xml` fix
  The Day 1 pom.xml referenced four non-existent Spring Boot test starter artifacts
  (`spring-boot-starter-flyway-test`, etc.). These were replaced with the standard
  `spring-boot-starter-test` which bundles JUnit 5, AssertJ, and MockMvc support.
  `org.testcontainers:postgresql` was added for the concurrency integration test.

  #### 2. `api` package (HTTP layer)

  Added five files to `src/main/java/.../api`:

  - `TransferRequest.java` — JSON request body record for `POST /transfers`
  - `TransferResponse.java` — JSON response; `failureReason` is omitted when null
  - `WalletResponse.java` — JSON response for `GET /wallets/{id}`
  - `ErrorResponse.java` — uniform error envelope `{ "error": "...", "message": "..." }`
  - `TransferController.java` — thin `@RestController` with three endpoints:
    - `POST /transfers` (core, required)
    - `GET /transfers/{id}` (optional)
    - `GET /wallets/{id}` (optional)
  - `GlobalExceptionHandler.java` — `@RestControllerAdvice` mapping:
    - `InvalidTransferRequestException` → 400
    - `WalletNotFoundException` → 404
    - `IdempotencyKeyReuseException` → 422
    - Unexpected `Exception` → 500

  #### 3. `TransferRepository` extensions

  - `findById` promoted from `private` to `public` (required by `GET /transfers/{id}`)
  - `findWalletById` added (required by `GET /wallets/{id}`)

  #### 4. Concurrency integration test

  `ConcurrentTransferIntegrationTest` runs against a real PostgreSQL 16 container
  started by Testcontainers. It fires eight concurrent transfer requests against the
  same source wallet and asserts the core money invariant:

  ```
  (processed count × amount) + final source balance == initial balance
  ```

  Also asserts: no negative balances, target balance == total credited, ledger row
  count == processed count × 2.

  #### 5. README

  Replaced the template README with full candidate documentation:
  - How to run locally (Docker + `./mvnw spring-boot:run`)
  - curl examples for all three endpoints
  - Test command (`./mvnw -B test`)
  - Error reference table
  - AI disclosure section

  ### Key design decisions finalized on Day 2

  **Insufficient funds → 200 + `state=FAILED`, not 409.**
  A FAILED transfer is a legitimate, idempotently-replayable outcome. Returning 409
  would mean retries need special handling to distinguish "client error" from
  "processed before". Returning 200 + FAILED keeps retries simple and safe.

  **No wallet creation API.**
  Wallet creation is explicitly out of scope. Test fixtures seed wallets directly.
  This is documented in the README.

  **Testcontainers over MockMvc for concurrency test.**
  The concurrency test must use real PostgreSQL locking. H2 does not support
  `SELECT ... FOR UPDATE` with the same semantics. Testcontainers starts a throwaway
  container that is torn down after the test suite.

