# Wallet Transfer Service

A production-grade wallet-to-wallet transfer service written in Go. Supports atomic transfers, double-entry bookkeeping, idempotent requests, and deadlock-safe concurrent access.

## Prerequisites

- Go 1.22+
- Docker (for local Postgres and integration tests)
- `golangci-lint` (`brew install golangci-lint` on macOS)

## Quick Start

```bash
# 1. Start Postgres
make up

# 2. Run migrations
make migrate-up

# 3. Build and run
make build
DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable" ./bin/wallet-transfer
```

The server listens on `:8080` by default. Override with `ADDR=:9000`.

## Environment Variables

| Variable          | Default                                                               | Description              |
|-------------------|-----------------------------------------------------------------------|--------------------------|
| `DATABASE_URL`    | `postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable` | Postgres connection URL  |
| `ADDR`            | `:8080`                                                               | HTTP listen address      |
| `MIGRATIONS_PATH` | `migrations`                                                          | Path to migration files  |

The server runs migrations automatically on startup.

## Testing

```bash
# Unit tests (no database required)
make test

# Integration tests (requires Docker — spins up Postgres via testcontainers)
make test-integration
```

Integration tests start an ephemeral `postgres:16-alpine` container, run all migrations, execute the test suite, and tear the container down automatically.

## Code Quality

```bash
make lint   # golangci-lint run ./...
make fmt    # gofmt -l -w .
```

## API Reference

### `POST /transfers`

Execute a wallet-to-wallet transfer.

**Request**
```json
{
  "idempotencyKey": "unique-client-key",
  "fromWalletId":   "uuid",
  "toWalletId":     "uuid",
  "amount":         "100.0000"
}
```

`amount` is a decimal string (4 decimal places). Using a string avoids float-precision issues at JSON and database boundaries.

**Responses**

| Status | Meaning |
|--------|---------|
| `201 Created` | Transfer processed successfully |
| `200 OK` | Idempotent replay of a previous success |
| `422 Unprocessable Entity` | Insufficient funds (also returned on replay of a failed transfer) |
| `409 Conflict` | Idempotency key reused with a different payload |
| `400 Bad Request` | Validation failure (missing field, invalid UUID, non-positive amount) |
| `500 Internal Server Error` | Unexpected failure |

**Response body (201/200/422)**
```json
{
  "transferId":   "uuid",
  "status":       "PROCESSED | FAILED",
  "fromWalletId": "uuid",
  "toWalletId":   "uuid",
  "amount":       "100.0000",
  "createdAt":    "2024-01-01T00:00:00Z",
  "processedAt":  "2024-01-01T00:00:00Z"
}
```

### `GET /healthz`

Liveness probe. Returns `200 OK` with `{"status":"ok"}` if Postgres is reachable, `503` otherwise.

---

## Design

### Architecture

Clean layered architecture with strict dependency direction:

```
handler  →  service  →  repository  →  postgres
           (domain)
```

- **domain** — pure Go entities, value objects, state machine, validation. No database or HTTP imports.
- **repository** — pgx queries. Every method accepts `ctx context.Context, tx pgx.Tx` — the service owns transaction boundaries.
- **service** — business logic, idempotency, transaction orchestration.
- **handler** — thin HTTP layer: JSON decoding, field validation, error-to-status mapping.

### Amount Representation

`domain.Amount` is a string-backed type that stores exactly 4 decimal places (`"100.0000"`). Arithmetic is done via `math/big.Rat` to avoid floating-point rounding. The database column is `NUMERIC(20, 4)` — the `CHECK (balance >= 0)` constraint is a hard backstop even if application logic were wrong.

### Concurrency Strategy

Concurrent transfers between the same wallets (e.g. A→B and B→A simultaneously) can deadlock if each transaction locks wallets in acquisition order.

We prevent this by **locking both wallets in deterministic UUID order** within a single query:

```sql
SELECT id, balance FROM wallets
WHERE id = ANY($1)
ORDER BY id
FOR UPDATE
```

Postgres acquires row locks in `ORDER BY` order, so all transactions lock the same wallet first regardless of transfer direction. The `CHECK (balance >= 0)` constraint provides a safety net if the lock ordering were ever violated.

### Idempotency

Every transfer request carries a client-supplied `idempotencyKey`. The flow:

1. `INSERT INTO idempotency_records ... ON CONFLICT (key) DO NOTHING` — within the same transaction as the transfer.
2. If the INSERT succeeded → proceed with the transfer, then update the idempotency record with the final HTTP status + response body.
3. If the INSERT was a no-op (key exists):
   - Hashes match → return the stored response verbatim (success replays as `200`, failure replays as `422`).
   - Hashes differ → return `409 Conflict`.

Because the idempotency record and the transfer are in the **same transaction**, a crash mid-flight rolls back both atomically — there is no state where a transfer exists without a matching idempotency record, or vice versa.

Insufficient funds is a *business failure*, not an application error. The transfer is recorded as `FAILED` and the `422` response is stored in the idempotency record, so retries receive the same `422` without re-executing any logic.

### Transfer Flow (service layer)

```
1.  Validate request (domain layer)
2.  BEGIN TRANSACTION
3.  INSERT idempotency record ON CONFLICT DO NOTHING
4.  On conflict → validate hash → replay or 409
5.  SELECT … FOR UPDATE both wallets ORDER BY id  (deadlock prevention)
6.  Verify both wallets exist
7.  Check balance ≥ amount; if not → INSERT FAILED transfer → COMMIT → 422
8.  INSERT transfer (PENDING)
9.  INSERT two ledger entries (DEBIT from, CREDIT to)
10. UPDATE wallet balances
11. UPDATE transfer → PROCESSED
12. UPDATE idempotency record with final response
13. COMMIT
```

### Double-Entry Ledger

Every successful transfer creates exactly two `ledger_entries` rows:

| entry_type | wallet | amount |
|------------|--------|--------|
| `DEBIT`    | from   | amount |
| `CREDIT`   | to     | amount |

The `UNIQUE (transfer_id, wallet_id, entry_type)` constraint prevents duplicate entries. The sum of all `DEBIT` amounts equals the sum of all `CREDIT` amounts across the entire ledger, ensuring total balance is conserved.

## Tradeoffs and Assumptions

A few decisions deserve explicit acknowledgment for the reviewer:

- **Decimal precision.** Amounts are stored to 4 decimal places. Inputs with more precision (e.g. `"100.00001"`) are silently truncated toward zero rather than rejected. A production implementation should either reject excess precision or document a rounding mode explicitly.

- **Currency.** Wallets carry a `currency` field, but the service does not reject cross-currency transfers — `amount` is treated as currency-agnostic. A production implementation would either enforce same-currency via DB constraint and service validation, or introduce an FX leg with a rate-snapshot ledger entry. Out of scope for this assignment.

- **Same-key concurrent requests serialize.** Two simultaneous requests with the same `idempotencyKey` will block each other on the `INSERT ... ON CONFLICT` lock — the second waits for the first to commit, then receives the stored response as a replay. This is correct but means heavy retry storms can pile up open connections. Acceptable for this design; an asynchronous front-line idempotency check (e.g. Redis) would be the next iteration.

- **No panic recovery middleware.** A defensive `recover()` in the HTTP layer is standard production hygiene but was omitted to keep the handler thin and the scope tight.

- **No metrics, tracing, or structured request logging.** Production observability concerns are out of scope.