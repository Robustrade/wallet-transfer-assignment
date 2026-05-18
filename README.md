# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## Core Features & Supported APIs

This repository provides a production-grade **Wallet Transfer Service** built in Go, designed with strict consistency, concurrency control, and clean architecture.

### Supported API Endpoints

The API is fully structured under `v1` routes:

#### 1. Wallets API
- **`POST /api/v1/wallets`**: Creates a new wallet for a user with a specified currency.
- **`GET /api/v1/wallets/:id`**: Retrieves wallet metadata, currency, and the current balance.
- **`GET /api/v1/wallets/:id/ledger`**: Returns a list of ledger entries for a wallet. Uses high-performance, offset-free **seek/cursor pagination** (`?limit=20&cursor=ID`) and returns the `next_cursor` to ensure O(1) query latency even under heavy volume.

#### 2. Transfers API
- **`POST /api/v1/transfers`**: Initiates an atomic, idempotent transfer from one wallet to another. Requires an `idempotencyKey`.
- **`GET /api/v1/transfers/:id`**: Fetches details and completion status of a transaction by its ID.

#### 3. Health Check
- **`GET /health`**: Simple health endpoint checking backend readiness.

---

### Core System Guarantees & Features

1. **Strict Idempotency (Exactly-Once Semantics)**:
   - Every transfer requires a client-provided `idempotencyKey`.
   - The service utilizes unique constraints and guarded transactional state machines (`PENDING`, `PROCESSED`, `FAILED`).
   - Duplicate or replayed requests safely bypass processing and directly return the cached HTTP response, preventing duplicate debits.

2. **Double-Entry Ledger Integrity**:
   - Every wallet transfer atomically records exactly two matching entries in the `ledger_entries` table: a `DEBIT` against the sender and a `CREDIT` to the receiver.
   - Balances are securely tracked alongside the ledger history, ensuring full auditability where the overall ledger must always balance to zero.

3: **Safe Concurrency & Deadlock Prevention**:
   - Executes each transfer inside a single database transaction. In the SQLite-backed implementation, concurrency control relies on SQLite transaction/write serialization (WAL mode with `sql.LevelSerializable` and single writer thread isolation) rather than row-level `SELECT ... FOR UPDATE` locks.
   - Wallets are read and updated in **ascending numeric wallet ID order** for every transfer (e.g. if transferring between 2 and 1, it processes 1 first, then 2). This deterministic ordering avoids inconsistent access patterns and preserves a clear migration path to engines (like PostgreSQL) that support explicit row-level locking.
   - Prevents double-spending, balance-skewing, and stale reads under highly concurrent transfer requests by ensuring debit, credit, balance updates, and ledger writes succeed or fail atomically together.

4. **JSON-Based Configuration**:
   - Application behaviors (database path and server port) are defined in a centralized `config.json` in the root workspace.
   - Supports automated fallback logic to environment variables for seamless testing and containerization.

5. **Advanced Production Architecture & Extensibility**:
   - **Transaction Reference IDs**: Generates non-sequential, trace-safe `reference_id` strings (e.g. `TXN_20260518_abcdef12`) combining dates and idempotency key prefixes. This completely avoids exposing database sequential primary keys (`uint64`) externally.
   - **Audit Metadata Storage**: Accepts optional `metadata` payloads (e.g. JSON containing client IP, device ID, source metadata) stored directly within the transaction context for advanced fraud tracking and auditing.
   - **Transactional Outbox Pattern**: Integrates event streaming using a local `outbox_events` table. Every successful transfer atomically writes a `TransferCompleted` event within the *exact same* database transaction block. This allows a background publisher to forward events to Kafka/RabbitMQ with exactly-once delivery guarantees and zero risk of inconsistency (Dual Write anomaly resolved).
   - **Redis Fast-Path Idempotency Cache**: Offloads the fast-path idempotency pre-check directly to a high-speed, in-memory **Redis** cache. Successful transaction payloads are cached with a 24-hour TTL, enabling up to **10k+ RPS** scale by bypassing SQL reads on duplicate requests.

6. **Clean Architecture**:
   - Enforces a distinct separation of concerns across four standard layers:
     - **Handler Layer**: Transport mapping, HTTP routing, and parsing using a centralized `internal/utils` package.
     - **Service Layer**: Transaction orchestration, business logic, and idempotency safeguards.
     - **Repository Layer**: Raw persistence operations and database interactions.
     - **Domain Layer**: Clean Go structures, schemas, and error boundaries.

---

## Local End-to-End Integration Testing

You can run comprehensive end-to-end integration tests on your local machine using our validated curls.

1. **Spin up local Redis cache (Docker)**:
   ```bash
   docker run -d --name wallet_redis -p 6379:6379 redis:alpine
   ```

2. **Initialize the local database**:
   ```bash
   sqlite3 wallet.db < migrations/001_init.sql
   sqlite3 wallet.db < migrations/seed.sql
   ```

3. **Start the Go server**:
   ```bash
   go run cmd/server/main.go
   ```

4. **Explore Interactive Swagger API Documentation**:
   Open your browser and navigate to:
   ```text
   http://localhost:8080/swagger/index.html
   ```

5. **Check the complete local curls & results**:
   Refer to our detailed [Local End-to-End Test Report](./e2e_test_report.md) for step-by-step curls, replays, health status, Redis caching checks, and double-entry ledger pagination.

---

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).
