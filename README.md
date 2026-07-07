# Wallet Transfer Service

A PostgreSQL-backed wallet transfer API written in Go. The implementation uses
Clean Architecture boundaries, stored wallet balances, an immutable
double-entry ledger, transactional idempotency, and row-level locking.

## Architecture

```text
HTTP handler -> transfer service -> repository contracts -> PostgreSQL adapter
                       |
                    domain
```

- `cmd/api`: dependency wiring and HTTP server lifecycle
- `internal/domain`: wallet, transfer, ledger, and state-transition models
- `internal/service`: transfer orchestration and business rules
- `internal/repository`: persistence and transaction contracts
- `internal/repository/postgres`: PostgreSQL implementation
- `internal/handler/http`: request validation and HTTP mapping
- `migrations`: database schema
- `tests/integration`: PostgreSQL behavior and concurrency tests

Amounts are signed 64-bit integers in the currency's smallest unit. Floating
point values are intentionally not accepted.

## Transaction and concurrency model

Each new request runs in one PostgreSQL transaction:

1. Insert the idempotency key and canonical request hash.
2. Create a `PENDING` transfer and link it to the idempotency record.
3. Lock both wallets with `SELECT ... FOR UPDATE`, ordered by wallet ID.
4. Validate funds, update both stored balances, and insert one `DEBIT` plus one
   `CREDIT` ledger entry.
5. Move the transfer to `PROCESSED`, or commit a deterministic business failure
   as `FAILED`.

The primary key on `idempotency_records.idempotency_key` serializes concurrent
duplicates. A duplicate with the same request hash reads the original terminal
transfer. Reusing the key for a different payload returns `409 Conflict`.
Infrastructure failures roll back the entire transaction and are safe to retry.

## Run with Docker Compose

Requirements: Docker with Compose support.

```bash
docker compose up --build
```

The migration is applied automatically when PostgreSQL initializes. Seed two
wallets:

```bash
docker compose exec postgres psql -U wallet -d wallet -c \
  "INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000), ('wallet_2', 250);"
```

Create a transfer:

```bash
curl -i http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "abc123",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }'
```

Re-send the same request to receive the original result without another debit.
Change the amount while retaining `abc123` to receive `409 Conflict`.

To delete local data and rerun the initialization migration:

```bash
docker compose down -v
```

## Run locally

Start PostgreSQL and apply the migration:

```bash
psql "$DATABASE_URL" -f migrations/001_init.sql
```

Set the database URL and run the API:

```bash
export DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
go run ./cmd/api
```

On PowerShell:

```powershell
$env:DATABASE_URL = 'postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
go run ./cmd/api
```

The API listens on `:8080` by default. Override it with `HTTP_ADDRESS`.

## API

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

Responses:

- `201 Created`: transfer is `PROCESSED`
- `400 Bad Request`: invalid input or JSON
- `404 Not Found`: one or both wallets do not exist; transfer is `FAILED`
- `409 Conflict`: idempotency key was used with a different payload
- `422 Unprocessable Entity`: insufficient balance; transfer is `FAILED`
- `500 Internal Server Error`: transaction was rolled back and may be retried

`GET /healthz` provides a process health endpoint.

## Tests

Run unit tests:

```bash
go test ./internal/...
```

Run every test. Integration tests skip when no database URL is configured:

```bash
go test ./...
```

Run PostgreSQL integration and concurrency tests against a dedicated test
database:

```bash
export INTEGRATION_DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet_test?sslmode=disable'
go test ./tests/integration -count=1
```

PowerShell:

```powershell
$env:INTEGRATION_DATABASE_URL = 'postgres://wallet:wallet@localhost:5432/wallet_test?sslmode=disable'
go test ./tests/integration -count=1
```

The integration suite truncates the wallet service tables between tests. Do not
point it at a database containing data you need.
