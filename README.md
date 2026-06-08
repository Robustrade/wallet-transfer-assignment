# Wallet Transfer Service

A Go service for wallet-to-wallet transfers with idempotency, double-entry ledger, and safe concurrent execution.

## Features

- `POST /transfers` — create a transfer with idempotency support
- `GET /health` — health check
- Double-entry ledger (DEBIT + CREDIT per transfer)
- Transfer state machine: `PENDING` → `PROCESSED` | `FAILED`
- PostgreSQL with transactional safety and row-level locking

## Tech Stack

- **Go 1.24**
- **PostgreSQL 16+**
- **pgx/v5** — database driver
- Layered architecture: handler → service → repository → domain

## Project Structure

```
cmd/
  server/       # HTTP server entrypoint
  migrate/      # migration runner
internal/
  domain/       # entities, validation, state transitions
  handler/      # HTTP layer
  service/      # business logic and orchestration
  repository/   # database access
  migrate/      # embedded SQL migrations
migrations/     # SQL migration files
```

## Prerequisites

- Go 1.24+
- PostgreSQL running locally (port 5432)
- Optional: Docker (if you prefer `docker compose`)

## Database Setup

### Option A: Use existing Postgres (recommended)

If you already have Postgres on `:5432` (e.g. `my-postgres`):

```bash
docker exec my-postgres psql -U postgres -c "CREATE DATABASE wallet_transfer;"
```

Default connection string:

```
postgres://postgres:root@localhost:5432/wallet_transfer?sslmode=disable
```

### Option B: Docker Compose

```bash
make up
```

Uses port **5433** to avoid conflicting with an existing Postgres on 5432:

```
postgres://postgres:root@localhost:5433/wallet_transfer?sslmode=disable
```

## Quick Start

```bash
# Install dependencies
go mod tidy

# Run migrations
make migrate

# Run tests
make test

# Start server
make run
```

Server listens on `:8080` by default. Override with `SERVER_ADDR`.

## Environment Variables

| Variable       | Default                                                                   | Description         |
|----------------|---------------------------------------------------------------------------|---------------------|
| `DATABASE_URL` | `postgres://postgres:root@localhost:5432/wallet_transfer?sslmode=disable` | PostgreSQL DSN      |
| `SERVER_ADDR`  | `:8080`                                                                   | HTTP listen address |

## API

### Health Check

```bash
curl http://localhost:8080/health
```

Response:

```json
{"status":"ok"}
```

### Create Transfer

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "abc123",
    "fromWalletId": "<source-wallet-uuid>",
    "toWalletId": "<dest-wallet-uuid>",
    "amount": 100
  }'
```

#### Request Body

| Field            | Type   | Required | Description                  |
|------------------|--------|----------|------------------------------|
| `idempotencyKey` | string | yes      | Unique key for deduplication |
| `fromWalletId`   | string | yes      | Source wallet UUID           |
| `toWalletId`     | string | yes      | Destination wallet UUID      |
| `amount`         | int64  | yes      | Transfer amount (> 0)        |

#### Response Codes

| Status | Meaning                                       |
|--------|-----------------------------------------------|
| `201`  | New transfer created and processed            |
| `200`  | Idempotent replay (same key, same payload)    |
| `400`  | Invalid request                               |
| `404`  | Wallet not found                              |
| `409`  | Idempotency key reused with different payload |
| `422`  | Insufficient funds (transfer marked FAILED)   |

#### Example Success Response

```json
{
  "id": "uuid",
  "idempotencyKey": "abc123",
  "fromWalletId": "uuid",
  "toWalletId": "uuid",
  "amount": 100,
  "status": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-06-08T12:00:00Z",
  "updatedAt": "2026-06-08T12:00:00Z"
}
```

### Seed Test Wallets

```bash
docker exec my-postgres psql -U postgres -d wallet_transfer \
  -c "INSERT INTO wallets (balance) VALUES (1000), (0) RETURNING id, balance;"
```

Use the returned UUIDs in your transfer request.

## Design Decisions

### Schema

| Table            | Purpose                                      |
|------------------|----------------------------------------------|
| `wallets`        | Stored balance (`BIGINT`, non-negative)      |
| `transfers`      | Transfer records with unique idempotency key |
| `ledger_entries` | Double-entry DEBIT/CREDIT audit trail        |

Key constraints:

- `UNIQUE(idempotency_key)` on transfers
- `CHECK(from_wallet_id <> to_wallet_id)`
- `CHECK(balance >= 0)` on wallets
- Foreign keys from transfers/ledger to wallets

### Idempotency

- `idempotency_key` stored with a **unique constraint** on `transfers`
- Duplicate requests with the same key return the original transfer (`200 OK`)
- Concurrent duplicate inserts handled via unique violation → fetch existing row
- Same key with different payload returns `409 Conflict`

### Concurrency

- `SELECT ... FOR UPDATE` on wallets inside a single DB transaction
- Wallets locked in **sorted UUID order** to prevent deadlocks
- Balance check and debit happen while the source row is locked
- Prevents double spending under concurrent debits

### Transfer Flow

```
1. Validate request
2. Check idempotency key (return existing if replay)
3. Lock source + destination wallets
4. Insert transfer (PENDING)
5. Check balance → FAILED if insufficient
6. Debit source, credit destination
7. Write DEBIT + CREDIT ledger entries
8. Mark transfer PROCESSED
```

## Testing

```bash
# All tests (requires Postgres)
make test

# Lint
make lint

# Format
make fmt
```

Tests cover:

- Transfer execution and balance updates
- Idempotency and conflict detection
- Ledger correctness (2 entries per transfer)
- Insufficient funds and wallet-not-found failures
- Concurrent debits (no double spend)

Integration tests skip automatically if Postgres is unavailable.

## Makefile Commands

| Command        | Description               |
|----------------|---------------------------|
| `make migrate` | Apply database migrations |
| `make run`     | Start HTTP server         |
| `make test`    | Run all tests             |
| `make lint`    | Run golangci-lint         |
| `make fmt`     | Format Go code            |
| `make up`      | Start Docker Postgres     |
| `make down`    | Stop Docker Postgres      |

## Tradeoffs / Assumptions

- Amounts stored as `BIGINT` (integer units, not floats)
- Balances stored on `wallets` row (not derived from ledger at read time)
- No separate `idempotency_records` table — unique key on `transfers` is sufficient
- Wallet-not-found returns `404` without creating a transfer record (FK-safe)
- Insufficient funds creates a `FAILED` transfer for audit/replay
- No wallet creation API (wallets seeded via SQL for now)

## Assignment Submission

See [`ASSIGNMENT.md`](./ASSIGNMENT.md) for full requirements.

Submit via PR to `main` on branch `solution/<your-name>`.
