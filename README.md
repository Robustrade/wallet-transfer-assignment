# Wallet Transfer Service

A small service that performs **wallet-to-wallet transfers** with a focus on
correctness under retries, duplicates, and concurrency: idempotent request
handling, double-entry ledger recording, safe state transitions, and
race-free balance updates.

## Design at a glance

- **Money is integer minor units** (`BIGINT`, e.g. cents). Floating point is
  never used for monetary values.
- **Stored balances** on the `wallets` row, updated inside the same transaction
  as the ledger entries, with the ledger kept as the immutable audit trail.
- **Idempotency** via a unique idempotency key, so a request is applied
  exactly once even under retries or concurrent duplicate delivery.
- **Concurrency safety** via `SELECT ... FOR UPDATE` row locks taken in a
  deterministic order, under the default `READ COMMITTED` isolation level.
- **Database-enforced invariants** (non-negative balance, positive amount,
  valid status, exactly one debit/credit per transfer) so correctness does not
  depend on application logic alone.

## Architecture

A clean layered architecture with one responsibility per layer:

```
cmd/server            process entry point and wiring
internal/handler      HTTP transport: decode, validate, map errors -> status
internal/service      transfer workflow: idempotency, lock ordering,
                      orchestration, state transitions; owns the transaction
internal/repository   persistence only: one SQL statement per method + WithTx
internal/domain       entities, the transfer state machine, validation rules
migrations            portable schema (standard SQL, runs on PostgreSQL/SQLite)
```

The entire transfer workflow lives in a single method,
`service.TransferService.CreateTransfer`, so the behavior reads top-to-bottom.
It runs as one atomic transaction via `repository.Store.WithTx`; the repository
performs no business decisions.

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

Response `201 Created`:

```json
{
  "id": "f1c2...",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED"
}
```

Behavior:

- Replaying the same `idempotencyKey` with the same body returns the original
  result and performs no second transfer.
- Reusing the same key with a different body is rejected (`409 Conflict`).
- Insufficient funds is recorded as a `FAILED` transfer (no balance change),
  which makes failures auditable and idempotent.

| Situation | Status |
|---|---|
| Transfer executed or replayed | `201` |
| Validation error (amount ≤ 0, same wallet, missing fields) | `400` |
| Wallet not found | `404` |
| Idempotency key reused with a different payload | `409` |

## Data model

| Table | Purpose | Key constraints |
|---|---|---|
| `wallets` | stored balance per wallet | `CHECK (balance >= 0)` |
| `transfers` | one row per transfer with status | `CHECK status IN (...)`, `amount > 0`, `from <> to` |
| `ledger_entries` | double-entry records | `UNIQUE (transfer_id, type)`, FK to `transfers` |
| `idempotency_records` | key → transfer mapping | `idempotency_key` PRIMARY KEY, `request_hash` |

The schema uses only standard SQL types so it runs unchanged on PostgreSQL and
SQLite.

## Running locally

Requires Go 1.24+ and a PostgreSQL instance.

```bash
# 1. Start PostgreSQL (example with Docker)
docker run --rm -d --name wallet-pg \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=wallet \
  -p 5432:5432 postgres:16-alpine

# 2. Apply the schema
psql "postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable" \
  -f migrations/0001_init.sql

# 3. Run the server
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable"
go run ./cmd/server
```

The server listens on `:8080` (override with `PORT`).

## Testing

```bash
# Domain tests need no database and always run:
go test ./internal/domain/...

# Service tests are behavioral integration tests against a real PostgreSQL.
# Point DATABASE_URL at a database; the suite applies the schema itself.
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable"
go test ./... -race            # full suite
go test ./... -race -count=5   # stress the concurrency tests
```

- **Domain tests** are pure and need no database.
- **Service tests** require `DATABASE_URL`. They run against a real PostgreSQL,
  which is necessary to exercise `SELECT ... FOR UPDATE` locking and the
  database constraints that a mock cannot reproduce. The test setup resets the
  schema and applies `migrations/0001_init.sql` automatically. **If
  `DATABASE_URL` is unset, these tests skip** rather than fail, so
  `go test ./...` is always green.

The suite covers transfer execution, idempotent replay, key-reuse conflict,
insufficient-funds (`FAILED`), and two concurrency tests: many concurrent
debits of one wallet (no double spend, never negative, ledger reconciles) and
concurrent duplicate delivery of the same key (exactly one transfer).
