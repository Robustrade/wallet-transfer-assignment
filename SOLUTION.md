# Wallet Transfer Service — Solution

A wallet-to-wallet transfer service built with Go (standard library) and PostgreSQL,
focused on transactional correctness, idempotency, and concurrency safety.

## Prerequisites

- Go 1.25+
- Docker (for PostgreSQL)

## Setup

1. Start PostgreSQL:

```bash
docker compose up -d
```

2. Apply the schema:

```bash
docker exec -i wallet_postgres psql -U wallet -d wallet_db < migrations/0001_init.sql
```

3. Configure environment. Create a `.env` file in the project root:

```env
DATABASE_URL=postgres://wallet:wallet@localhost:5433/wallet_db?sslmode=disable
HTTP_ADDR=:8090
```

4. Run the server:

```bash
go run ./cmd/server
```

## Environment Variables

| Variable       | Description                      | Default                                                             |
|----------------|----------------------------------|---------------------------------------------------------------------|
| `DATABASE_URL` | PostgreSQL connection string     | `postgres://wallet:wallet@localhost:5433/wallet_db?sslmode=disable` |
| `HTTP_ADDR`    | Address the HTTP server binds to | `:8090`                                                             |

## API

### Create Transfer

POST /transfers

Request:

```json
{
  "idempotencyKey": "key1",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Responses:

| Status | Meaning                                             |
|--------|-----------------------------------------------------|
| 201    | Transfer created                                    |
| 200    | Duplicate idempotency key; original result returned |
| 400    | Validation error (bad amount, same wallet, etc.)    |
| 404    | Wallet not found                                    |
| 422    | Insufficient balance                                |
| 500    | Internal error                                      |

Example:

```bash
curl -X POST http://localhost:8090/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"key1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

### Health Check

GET /healthz

## Wallet Provisioning

Wallet creation is out of scope for this assignment (only `POST /transfers` is required).
Seed wallets directly for testing:

```bash
docker exec -i wallet_postgres psql -U wallet -d wallet_db \
  -c "INSERT INTO wallets (id, balance) VALUES ('wallet_1', 500), ('wallet_2', 0);"
```

## Running Tests

```bash
go test ./...
```

The concurrency integration test requires a running PostgreSQL and is skipped
automatically if the database is unreachable. The race detector requires cgo;
on platforms with a C compiler available:

```bash
CGO_ENABLED=1 go test -race ./...
```

## Architecture

The codebase follows a clean layered architecture:

```
cmd/server         entrypoint: config, wiring, HTTP server
internal/handler   HTTP transport: decode, call service, map errors to status codes
internal/service   business logic: transfer workflow, idempotency, orchestration
internal/repository persistence: pgx queries, transaction manager, row locking
internal/domain    entities, state machine, validation rules
```

Dependencies point inward: handler → service → repository → domain. The domain
layer has no external dependencies.

## Design Decisions

### Money representation

Amounts are stored as `BIGINT` in minor units (e.g. cents). No floating point is
used for money, avoiding rounding errors.

### Balance strategy

Balances are stored on the `wallets` table and updated within each transfer,
rather than derived from the ledger. This gives O(1) balance reads. Correctness
under concurrency is enforced via row-level locking (see below).

### Transaction boundary

The `TxManager.WithTx` abstraction owns the transaction. The service opens a
single transaction and performs the entire workflow through a transaction-bound
`Repository`. If any step returns an error, the whole transaction rolls back.
This keeps the transaction boundary visible in the service while SQL stays in
the repository.

### State machine

Transfers follow `PENDING -> PROCESSED` or `PENDING -> FAILED`. Transitions are
validated in the domain layer; invalid transitions are rejected without mutating
state.

## Tradeoffs

- Stored balance favors fast reads but requires careful locking; a ledger-derived
  balance would be simpler to reason about but slower to query.
- Wallet creation, balance, and history APIs are intentionally out of scope to
  keep focus on transfer correctness.
- The concurrency test uses `t.Skip` when the DB is down rather than spinning up
  a container, keeping the test suite lightweight.