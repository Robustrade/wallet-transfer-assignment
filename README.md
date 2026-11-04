# Wallet Transfer Service

A synchronous Go HTTP service for wallet-to-wallet transfers backed by PostgreSQL. The implementation focuses on transactional correctness, idempotent request handling, concurrency safety, and a double-entry ledger.

## Features

- `POST /transfers` with an idempotency key supplied in the `Idempotency-Key` header or JSON body.
- Replay of the original result for the same key and request; conflicting payloads are rejected.
- Atomic wallet balance updates, transfer state changes, ledger entries, and idempotency response persistence.
- PostgreSQL row-level locking in deterministic wallet-ID order to prevent concurrent overspending and reduce deadlocks.
- Double-entry ledger records for successful transfers.
- Guarded transfer transitions: `PENDING -> PROCESSED` or `PENDING -> FAILED`.
- Embedded, versioned SQL migrations and a database readiness endpoint.

## Architecture

```text
cmd/server             application wiring and HTTP lifecycle
internal/handler       HTTP parsing, validation and response mapping
internal/service       transfer workflow, idempotency and transaction orchestration
internal/repository    PostgreSQL persistence operations
internal/domain        wallet, transfer, ledger types and business invariants
internal/database      PostgreSQL configuration and connection pool
migrations             embedded SQL migrations and migration runner
docs                   design, API, persistence and operational documentation
```

The service uses stored wallet balances. Balance changes and the corresponding ledger entries are committed in the same PostgreSQL transaction.

## Requirements

- Go 1.23 or newer
- PostgreSQL 16 or compatible
- A PostgreSQL database and role with permission to create and update the application schema

## Run locally

Set the connection string and optional HTTP address:

```sh
export DATABASE_URL='postgres://<user>:<password>@localhost:5432/<database>?sslmode=disable'
export HTTP_ADDR=':8080'
go run ./cmd/server
```

The application applies pending embedded migrations during startup. See [docs/run-local.md](docs/run-local.md) and [docs/database.md](docs/database.md) for setup details.

## API

`POST /transfers`

```sh
curl -i http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: transfer-demo-001' \
  -d '{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1000}'
```

Amounts are positive integers in the smallest currency unit (for INR, paise). A successful transfer returns `201 Created`. Deterministic business failures such as insufficient funds return `422 Unprocessable Entity`; reusing an idempotency key with a different payload returns `409 Conflict`.

See [docs/http-api.md](docs/http-api.md) for the full request and response contract.

## Tests

Unit tests:

```sh
go test ./...
go test -race -cover ./...
go vet ./...
```

PostgreSQL integration tests use an isolated temporary schema:

```sh
export TEST_DATABASE_URL="$DATABASE_URL"
go test -v ./internal/integration
```

The test database role must be allowed to create and drop schemas. Use a disposable test database rather than a production database. See [docs/run-local.md](docs/run-local.md).

## Design and operational notes

- [Transfer design and consistency model](docs/wallet-transfer-design.md)
- [HTTP API contract](docs/http-api.md)
- [Idempotency and retry behavior](docs/idempotency.md)
- [Database schema and constraints](docs/database.md)
- [Transfer service workflow](docs/transfer-service.md)
- [Local setup, migrations, health checks and shutdown](docs/run-local.md)

## Scope and limitations

This is a synchronous assignment implementation, not a complete payment platform. It does not provide authentication, wallet creation/funding APIs, asynchronous processing, multi-currency conversion, metrics, distributed tracing, or idempotency-record expiry. `PENDING` is an intermediate state within the database transaction and is not a durable queued state.

## AI usage disclosure

The assignment requires disclosure of AI assistance, including the tool used, how it was used, and a transcript or prompts. See [docs/ai-usage.md](docs/ai-usage.md). The transcript/prompt appendix must be reviewed and completed before submission.
