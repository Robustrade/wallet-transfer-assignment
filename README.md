# Wallet Transfer Service

A minimal wallet-to-wallet transfer service with idempotency, double-entry ledger, and concurrency safety.

## Quick Start

```bash
# Start PostgreSQL
docker compose up -d
go run ./cmd/server

# Or use SQLite (for development/testing without Docker)
DATABASE_URL=":memory:" go run ./cmd/server
```

## API

### Health Check

```
GET /health
```

### Create Transfer

```
POST /transfers
Content-Type: application/json

{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

- **201 Created** — transfer processed
- **200 OK** — duplicate request (same idempotency key)
- **400** — invalid request
- **422** — insufficient funds

### Get Wallet Balance

```
GET /wallets/{walletId}
```

Returns wallet with current balance.

## Testing

```bash
go test ./... -race -cover
```

Tests use an isolated SQLite file database (auto-cleaned). Four test cases:
- **Success** — transfer execution and balance verification
- **Idempotency** — duplicate key returns original result
- **Insufficient funds** — proper error handling  
- **Concurrency** — 10 concurrent transfers without double spending

## Architecture

```
handler  →  service  →  repository  →  database
 (HTTP)     (business)  (persistence)  (Postgres/SQLite)
```

- **Handler** — request validation, transport mapping
- **Service** — business logic, idempotency, transaction orchestration
- **Repository** — raw SQL, database operations
- **Domain** — entity types, state machine, validation errors

## Database Schema

- `wallets` — stored balance (denormalized for fast reads), updated atomically
- `transfers` — idempotency_key has UNIQUE constraint, state machine (PENDING → PROCESSED/FAILED)
- `ledger_entries` — immutable double-entry records (DEBIT + CREDIT per transfer)
- `idempotency_records` — tracks processed idempotency keys

## Design Decisions

### Idempotency
- `transfers.idempotency_key` has a UNIQUE constraint — the database prevents true duplicates
- Idempotency check before transaction (fast path for retries)
- If a concurrent duplicate slips past the check, the UNIQUE constraint catches it and returns the existing result

### Concurrency
- Transactions use `SERIALIZABLE` isolation level
- Wallet balance updated with atomic SQL (`balance = balance + delta`) — no read-then-write race
- Retry logic with backoff handles transient serialization failures
- **No explicit row locks** — relies on PostgreSQL's serialization or SQLite's built-in locking

### Stored Balance
- Balance is stored in the `wallets` table (not derived from ledger) for O(1) reads
- Updated atomically within the same transaction as ledger entries — consistency guaranteed by the database

## Assumptions & Tradeoffs

- **Stored balance over derived**: simpler and faster, slightly denormalized. For a financial system with audit requirements, a derived balance would be safer
- **SERIALIZABLE isolation**: simplest correct approach. Under high contention, PostgreSQL may abort transactions requiring retries (handled by the retry loop)
- **SQLite for tests**: convenient but has limited concurrent write throughput. The concurrent test uses a semaphore (max 3 concurrent) to work reliably with SQLite
