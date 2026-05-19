# Submission

This document summarizes the code submission for the wallet transfer assignment: the exposed APIs, project structure, how to run the project, and the design strategies used to implement a safe, idempotent wallet transfer system.

## APIs

All endpoints are JSON-based and use HTTP status codes to denote success/failure.

- POST /transfers
  - Description: Initiate a transfer between two wallets.
  - Request body (TransferRequest):
    - `idempotency_key` (string) — client-provided idempotency key to guarantee exactly-once semantics.
    - `from_wallet_id` (string) — source wallet ID.
    - `to_wallet_id` (string) — destination wallet ID.
    - `amount` (integer) — transfer amount in the smallest currency unit (e.g., cents).
  - Response (on success):
    - `status` — `processed` | `pending` | `failed`
    - `transaction_id` — internal transaction id for lookup.

- POST /wallets
  - Description: Create a wallet (test/helper API used by evaluators/tests).
  - Request body (CreateWalletRequest):
    - `wallet_id` (string) — ID to create.
    - `initial_balance` (integer) — starting balance in smallest units.
  - Response: created wallet metadata.

- GET /wallets/:id
  - Description: Fetch wallet info (balance and metadata).
  - Path param: `id` — wallet id.
  - Response (WalletResponse):
    - `wallet_id` (string)
    - `balance` (integer)

Note: DTOs and request/response shapes live in `internal/api/dtos` for quick reference.

## Project Structure

Top-level structure (important files/folders):

- `cmd/` — application entrypoint(s).
  - `cmd/server/main.go` — server bootstrap and route registration.
- `internal/` — application code (handler, service, repository, models).
  - `internal/api/` — HTTP handlers and route wiring.
  - `internal/wallet/` — domain models, repository and service implementing transfer logic.
- `migrations/` — goose-compatible SQL migrations to create tables, indices etc.
- `docs/` — documentation (this file and runtime guides).

See `internal` for separation of concerns (handlers -> service -> repository -> models).

## How to Run

Please consult the run guide for detailed steps and environment requirements:

- See: `docs/HOW_TO_RUN.md`

Common quick steps (example):

1. `docker compose up -d` (Runs db migrations as well)
2. Run migrations (this project uses goose SQL migrations):

```bash
# Example: run the migrator container or goose CLI configured in the repo
docker compose run --rm -it migrator
```

3. Use the helper APIs to create wallets and test transfers, or call the `/transfers` endpoint with an `idempotency_key`.

### Running Integration Tests

These integration tests exercise the running service (no isolated testcontainer required). They call the HTTP API at `http://localhost:8080` and clean up rows they create in teardown.

- Start the stack (run migrations + service):

```bash
docker compose up -d
```

- Ensure the tests can reach the database. Set `DATABASE_URL` to match your docker-compose Postgres service if necessary. Example (defaults used by the test suite):

```bash
export DATABASE_URL="postgres://postgres:Test001@localhost:5432/wallets?sslmode=disable"
```

- Run the integration tests:

```bash
go test ./integration -v
```

Notes:
- The tests expect the service to be reachable at `http://localhost:8080` (this is the default server port).
- The test suite will attempt to connect to `DATABASE_URL` and will use a reasonable default if the env var is not set; prefer explicitly setting `DATABASE_URL` to avoid surprises.
- Tests self-clean the rows they create (wallets, transactions, ledger_entries). Still, run tests against a dedicated test database to avoid interfering with production data.

## Key Design & Implementation Strategies

This section explains the important strategies used to make the wallet transfer system safe, reliable and testable.

- Transactional Safety
  - All transfer operations execute inside a single database transaction. The repository layer controls transaction boundaries; handlers call services which call repository methods.
  - Transaction isolation is set to `READ COMMITTED`.

- Pessimistic Locking and Deadlock Avoidance
  - Wallet rows involved in a transfer are locked with `SELECT ... FOR UPDATE` to serialize balance reads/updates and avoid lost updates.
  - To avoid deadlocks under concurrent transfers, wallets are always locked in a deterministic order (ascending wallet id) before performing any balance checks or updates.

- Idempotency
  - Transfers accept a client-supplied `idempotency_key`. The `transactions` table enforces a UNIQUE constraint on `idempotency_key`.
  - The code path inserts a `PENDING` transaction row and treats unique-violation errors (SQL state 23505) by fetching and returning the existing transaction rather than creating a duplicate.

- Double-Entry Ledger
  - Every transfer creates two `ledger_entries` rows: one `DEBIT` for the source and one `CREDIT` for the destination. This ensures a clear audit trail and balance reconciliation.
  - Ledger entries reference the `transactions` row so the system can always derive which ledger entries belong to a transfer.

- Atomic Balance Updates
  - Wallet balances are updated inside the same transaction that inserts ledger entries and updates the transaction state to `PROCESSED` or `FAILED`.
  - This provides ACID semantics: either the transfer (debit, credit, ledger entries, transaction state) is fully applied, or nothing is.

- Separation of Concerns
  - Handlers: parse and validate HTTP requests, map inputs to service DTOs, and return HTTP responses.
  - Services: business logic, input validation, higher-level orchestration, and idempotency decisions where appropriate.
  - Repositories: low-level DB access, SQL statements, and transaction management.
  - Models: domain types and enums (transaction state and ledger entry type) live under `internal/wallet`.

- Error Handling and Observability
  - Errors return appropriate HTTP status codes and messages. Idempotent conflicts return the existing transaction rather than a 500.
  - The code is structured to make it easy to add logging/metrics around repository operations (recommended next step).

- Migrations and DB Setup
  - Migrations are implemented as goose-compatible SQL files located in `migrations/`.
  - The primary migration creates the `wallets`, `transactions`, and `ledger_entries` tables and the supporting enums.

## Next Steps (Suggestions)

- Add structured error handling (currently everything is raw golang errors)
- Add a reconciler or background job to detect and resolve stale `PENDING` transactions if necessary.
- Add structured logging and metrics for transfer latencies, transaction failures, and contention hotspots.

