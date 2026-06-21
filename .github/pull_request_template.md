## Summary

This repository contains a robust, reliable, and concurrency-safe **Wallet Transfer Service** built using **Java 17**, **Spring Boot 3.3.1**, **Spring Data JPA**, and **SQLite**. 

The service exposes:
* `POST /transfers`: Executes wallet-to-wallet transfers with double-entry ledger logging, idempotency key checks, and transaction lock protections.
* `GET /wallets/{id}`: Retrieves wallet records to inspect balances (added for manual testing and ease of evaluation).

---

## AI disclosure

1. **What tool you used**: Claude Code (an agentic AI coding assistant by Anthropic).
2. **How you generally use the tool**: Pair programming. The agent analyzed the assignment requirements, proposed a structured implementation plan, scaffolded all project files (schema, domain entities, repositories, service layer, REST controllers, exception handlers), diagnosed and resolved transactional rollback edge cases, tuned SQLite JDBC connection parameters for concurrency, authored a multi-threaded JUnit 5 integration test suite, and confirmed successful compilation and local execution.
3. **Transcript / Prompt Log**:
   * "Please analyze this repository and provide a detailed summary of the assignment requirements and what needs to be implemented."
   * "The assignment mentions Go, but I'm more comfortable with Java. Can you evaluate both options against the requirements — concurrency model, ecosystem support, and ease of implementing pessimistic locking with a relational database?"
   * "Based on that evaluation, recommend the better choice for this assignment and outline a high-level implementation plan covering the domain model, service layer, API design, idempotency handling, and concurrency strategy."
   * "Before we proceed, let's run the existing tests to establish a baseline and verify the build is healthy."
   * "I've reviewed the generated `TransferService` and made some refinements — the deadlock prevention logic looks good, but I want to ensure the idempotency record is always persisted even when the transfer fails due to insufficient funds. Can you adjust the transaction boundary so the idempotency entry commits independently of the business exception?"
   * "Please update `IMPLEMENTATION_PLAN.md` to reflect the architecture decisions we've made — include schema design, idempotency strategy, and concurrency approach."
   * "Update the pull request description to align with our finalized implementation plan and accurately describe the schema, concurrency, and idempotency strategies."

---

## Schema Design

We introduced 4 tables in `schema.sql` to model the transactional data model:

1. **`wallets`**:
   * `id` (`VARCHAR(255) PRIMARY KEY`)
   * `balance` (`BIGINT NOT NULL CHECK (balance >= 0)`)
   * *Note*: The check constraint prevents negative balances at the database tier.
2. **`transfers`**:
   * `id` (`VARCHAR(255) PRIMARY KEY`)
   * `from_wallet_id` (`VARCHAR(255) REFERENCES wallets(id)`)
   * `to_wallet_id` (`VARCHAR(255) REFERENCES wallets(id)`)
   * `amount` (`BIGINT NOT NULL CHECK (amount > 0)`)
   * `state` (`VARCHAR(50) NOT NULL`): `PENDING`, `PROCESSED`, or `FAILED`.
   * `idempotency_key` (`VARCHAR(255) UNIQUE`)
   * `created_at` (`TIMESTAMP NOT NULL`)
3. **`ledger_entries`**:
   * `id` (`VARCHAR(255) PRIMARY KEY`)
   * `wallet_id` (`VARCHAR(255) REFERENCES wallets(id)`)
   * `transfer_id` (`VARCHAR(255) REFERENCES transfers(id)`)
   * `type` (`VARCHAR(50) NOT NULL`): `DEBIT` or `CREDIT`
   * `amount` (`BIGINT NOT NULL`)
   * `created_at` (`TIMESTAMP NOT NULL`)
4. **`idempotency_records`**:
   * `idempotency_key` (`VARCHAR(255) PRIMARY KEY`)
   * `request_hash` (`VARCHAR(255) NOT NULL`): MD5/concatenated string to catch parameter mismatches.
   * `response_status` (`INTEGER`): Stores response code.
   * `response_body` (`TEXT`): Serialized JSON response string.
   * `created_at` (`TIMESTAMP NOT NULL`)

---

## Idempotency Strategy

We implemented a durable **Deduplication Table** approach:
1. **Deduplication Check**: When a request starts, we search `idempotency_records` by key.
2. **Parameters Conflict Check**: If the key is found, we verify that the request parameters match the original parameters (via `request_hash`). If they don't, we throw `400 Bad Request` (`IdempotencyConflictException`).
3. **Replay Response**: If details match and `response_status` exists, we return the cached response immediately. If `response_status` is missing, the request is currently in-progress, and we return `409 Conflict`.
4. **Constraint Safe Deduplication**: If the key is new, we insert a pending record. A primary key constraint on `idempotency_records(idempotency_key)` protects against simultaneous concurrent requests using the same key.
5. **No Rollback for Business Exceptions**: Using `@Transactional(noRollbackFor = InsufficientFundsException.class)` allows transaction completions when funds are low so that the `FAILED` transfer state and corresponding `400 Bad Request` error responses are saved and replayed correctly for retries.

---

## Concurrency Strategy

1. **Pessimistic Locking**:
   * Wallets are loaded using `@Lock(LockModeType.PESSIMISTIC_WRITE)` to lock the rows during balance checking and updating.
2. **Deadlock Prevention (Sorted Locks)**:
   * To prevent deadlocks when concurrent transfers are executed in opposite directions (e.g. A -> B and B -> A), the service sorts the two wallet IDs lexicographically and locks the lower ID first, then the higher ID.
3. **SQLite Concurrency Optimization**:
   * Enabled **WAL** mode (`journal_mode=WAL`) to allow concurrent reads during active writes.
   * Configured **`transaction_mode=IMMEDIATE`** in the JDBC connection URL. This forces SQLite transactions to acquire write-locks on start, avoiding database lock upgrading deadlocks (`SQLITE_BUSY`) during concurrent multi-threaded requests.
   * Set `busy_timeout=10000` to allow threads to wait up to 10 seconds for locks.

---

## How to Run

1. **Verify environment**: Ensure you have Java 17+ and Maven installed.
2. **Run Server**:
   ```bash
   mvn spring-boot:run
   ```
3. **Send Transfer**:
   ```bash
   curl -X POST http://localhost:8080/transfers \
     -H "Content-Type: application/json" \
     -d '{"idempotencyKey":"manual-test-key","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
   ```
4. **Check Balance**:
   ```bash
   curl http://localhost:8080/wallets/wallet_1
   ```

---

## How to Test

Run the full automated test suite containing 8 integration, balance consistency, and 12-thread concurrent load tests:
```bash
mvn clean test
```

---

## Tradeoffs / Assumptions

* **SQLite choice**: Chosen because it is a lightweight, zero-configuration SQL database perfectly suited for a local developer environment setup. Optimized using WAL mode and IMMEDIATE transactions to offset SQLite's normal single-writer concurrency limits.
* **Double-Entry Ledger**: The ledger balances itself perfectly. For every transfer, exactly 2 entries (DEBIT from sender, CREDIT to receiver) are written.
* **Database Agnostic Locking**: Standard JPA and Hibernate configurations are used, making it simple to migrate this exact schema and locking setup to Postgres/MySql if needed.

---

## Checklist

- [x] Tests pass
- [x] Lint passes
- [x] Format check passes
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
