## Summary

Implemented a PostgreSQL-backed wallet transfer service with:

- Wallet creation and balance/history endpoints.
- Atomic wallet-to-wallet transfers.
- Double-entry ledger records for successful transfers.
- Durable idempotency claims and replayed responses.
- Request-hash conflict detection.
- Deterministic wallet lock ordering to prevent lost updates and deadlocks.
- Configurable idempotency claim retry attempts.
- Optional HTTPS support controlled by `SSL_ENABLED`.
- Database-free unit and HTTP tests using repository mocks.
- PostgreSQL integration tests for real transaction, locking, and idempotency behavior.

## AI Disclosure

Tool used: GitHub Copilot in Visual Studio Code.

I used Copilot as a coding assistant to create the base of this codebase then inspected the repository, identified risks in transaction and idempotency behavior and then implemented focused changes, and run validation commands. I reviewed the generated suggestions and kept the implementation aligned with the existing Go, PostgreSQL, repository, service, and controller structure.

Also after reviewing the code added config related changes to take values from env variables, decided to use postgres and added code for handling idempotency, preventing deadlock, and gave scenarios to copilot to handle error in different situations, then copilot changes code accordingly to handle those errors.

The session included prompts about configuring `MAX_CLAIM_ATTEMPTS`, adding SSL configuration, using PostgreSQL as the production database, mocking database calls during normal tests, explaining transaction and locking behavior, including the missing wallet ID in errors, testing live concurrent transactions and deadlocks, reviewing the implementation against the evaluation guide, fixing formatting and CI issues, and measuring coverage.

## Schema Design

The schema contains:

- `wallets`: wallet identity and maintained non-negative balance.
- `transfers`: transfer request, source and destination wallets, amount, state, failure reason, and unique idempotency key.
- `ledger_entries`: debit and credit entries linked to a transfer and wallet.
- `idempotency_records`: request hash, claim state, completed response status/body, and completion timestamp.

Constraints and indexes include:

- Primary keys on all entities.
- Foreign keys from transfers and ledger entries to wallets/transfers.
- `UNIQUE` constraint on `transfers.idempotency_key`.
- Primary key on `idempotency_records.key`.
- Checks for non-negative wallet balances, positive amounts, valid transfer states and ledger entry types, and different source/destination wallets.
- Indexes for transfer history by source/destination wallet and ledger lookup by wallet/transfer.

## Idempotency Strategy

Each request must contain an `idempotencyKey`. The service hashes the transfer payload and claims the key in PostgreSQL before mutating wallets.

The claim uses `INSERT ... ON CONFLICT DO NOTHING RETURNING`. If the key already exists, the existing record is read with `FOR UPDATE`:

- A completed identical request replays the stored response without repeating side effects.
- A different payload returns `409 idempotency_key_conflict`.
- An incomplete claim returns a retryable `409 idempotency_in_progress` response.
- Claims are rolled back when validation fails before a transfer is recorded.
- Insufficient funds are recorded as a terminal `FAILED` transfer and consume the key.

The transfer response is stored with the idempotency record so a later request can return the original transfer ID, status, and ledger response.

## Concurrency Strategy

Every transfer runs inside one PostgreSQL transaction. Both wallets are selected with `SELECT ... FOR UPDATE` before balances are read or changed.

Wallet IDs are sorted before locking, regardless of transfer direction. Therefore, concurrent transfers A-to-B and B-to-A acquire locks in the same order and cannot form a circular wait. Balance updates, transfer creation, ledger writes, state transitions, and idempotency completion commit atomically.

Normal tests use mocks so they do not require database connectivity. PostgreSQL integration tests are separate because they specifically verify behavior that only a real PostgreSQL database can provide, including row-level locks, transaction isolation, rollback, migrations, and database constraints. By default, those tests start a disposable PostgreSQL container through Testcontainers; `TEST_DATABASE_URL` can be supplied to use an existing PostgreSQL instance.

## How to Run

```bash
#set environment variables
 export DATABASE_URL="postgres://admin:admin123@localhost:5432/mydb?sslmode=disable" <postgres://username:password@hostname:port/databseName?sslmode=disable>
 export PORT=8080 <give the desired port number>
 export SSL_ENABLED=false <if needed SSL then configure true>
 export MAX_CLAIM_ATTEMPTS=3 

#Run the code using below command
go run ./cmd/server
```

The API starts on port `8080` by default. PostgreSQL must be available through `DATABASE_URL`.

For HTTPS, set `SSL_ENABLED=true`. Provide `TLS_CERT_FILE` and `TLS_KEY_FILE`, or allow the application to generate a development self-signed certificate.

## How to Test

Database-free tests:

```bash
make test
go test ./... -count=1
```

Formatting:

```bash
make fmt-check
gofmt -l .
git diff --check
```

PostgreSQL integration tests:

```bash
go test -tags=integration ./internal/repository/postgres/... -v
# or
make test-integration
```

The integration suite requires Docker because Testcontainers starts a disposable PostgreSQL 16 container automatically. To use an existing database instead, set `TEST_DATABASE_URL` before running the test command. The bundled `docker-compose.yml` remains available for local API development through `make up`.

Coverage:

```bash
go test ./... -coverpkg=./... -coverprofile=coverage-all.out
go tool cover -func=coverage-all.out
```

## Tradeoffs / Assumptions

- PostgreSQL is the only production persistence backend.
- Mocks are used for fast, database-free unit and HTTP tests; they are not intended to replace PostgreSQL integration tests.
- Amounts are signed 64-bit integers in the smallest currency unit.
- `READ COMMITTED` plus explicit row locks is used instead of `SERIALIZABLE`, because the transfer access pattern locks all mutated wallets in a consistent order.
- `POST /wallets` is a provisioning convenience for tests and demos.
- Insufficient funds is a terminal business result rather than a transport error and is returned as a failed transfer.
- Integration tests use Testcontainers by default and can use an existing database through `TEST_DATABASE_URL`.
- Local lint execution depends on `golangci-lint` being installed; the CI workflow provides the configured lint command and Go version.

## Checklist

- [x] Default tests pass.
- [ ] Lint passes locally; `golangci-lint` must be installed to run it.
- [x] Format check passes.
- [x] README and design notes updated.
- [x] PR description explains schema, idempotency, and concurrency.
- [x] PostgreSQL deadlock regression test added.
- [ ] PostgreSQL integration suite executed locally; requires `TEST_DATABASE_URL`.
