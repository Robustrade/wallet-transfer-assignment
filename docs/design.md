# Wallet Transfer Service — Design Notes

## Problem

Implement wallet-to-wallet transfers with idempotent API semantics, double-entry ledger, and safe concurrent debits.

## Architecture

- **Gin** — HTTP routing, middleware (recovery, request ID, access logs)
- **Handler** — JSON binding, status mapping only; no business logic
- **Service** — transfer workflow, idempotency, deadlock retry
- **Repository** — PostgreSQL transactions and row locks

## API Contract

### POST /transfers

Creates a transfer. Amounts are integer minor units (e.g. cents).

- **201 Created** — first successful request
- **200 OK** — idempotent replay (same `idempotencyKey`)
- **400** — validation error
- **404** — wallet not found
- **422** — insufficient funds (transfer recorded as `FAILED`)

### GET /swagger/index.html

Interactive Swagger UI (Swagger 2.0 / OpenAPI 2.0, generated via swaggo).

### GET /health

Returns `{"status":"ok"}` when PostgreSQL is reachable.

### GET /wallets/{id}/balance

Returns current stored balance for a wallet.

## State Machine

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

Terminal states (`PROCESSED`, `FAILED`) are immutable. Response snapshots are stored on the transfer row for durable replay.

## Idempotency

1. `idempotency_key` is **UNIQUE** on `transfers`.
2. Within a transaction, the service looks up the key with `SELECT ... FOR UPDATE`.
3. On replay, the stored `response_snapshot` JSON is returned — no duplicate ledger entries.
4. If two requests race on the same key, the loser hits the unique constraint and reloads the winner's row.

## Concurrency

1. Wallets are locked with `SELECT ... ORDER BY id FOR UPDATE` before creating a transfer row (consistent lock ordering).
2. Balance check, debit, credit, and ledger inserts happen in a single transaction.
3. Deadlocks are retried up to 3 times (clients should also retry with the same idempotency key).

## Failure Modes

| Scenario | HTTP | DB effect |
|----------|------|-----------|
| Missing idempotency key | 400 | none |
| Same wallet | 400 | none |
| Wallet not found | 404 | none |
| Insufficient funds | 422 | `FAILED` transfer, no ledger |
| DB error mid-tx | 500 | rolled back |

## Observability

- Structured JSON logs via `slog`
- Request ID via Gin middleware (`X-Request-ID`)
- Events: `transfer.processed`, `transfer.duplicate`, `transfer.failed`

## Testing Strategy

- Unit tests for validation and snapshot round-trip
- Integration tests (`-tags=integration`) with testcontainers PostgreSQL
- Concurrency test: 10 × 200 from balance 1000 → exactly 5 successes
