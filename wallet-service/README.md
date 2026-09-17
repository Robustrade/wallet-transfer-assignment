# Wallet Transfer Service

A reliable wallet-to-wallet transfer service built with **Go (standard library)** and **PostgreSQL**, demonstrating idempotency, concurrency safety, double-entry ledger, and clean architecture.

## Architecture

```
cmd/server/main.go          → entry point, dependency wiring
internal/handler/            → HTTP handlers (thin, validation + routing)
internal/service/            → business logic, idempotency, orchestration
internal/repository/         → database access (SQL queries)
internal/domain/             → entities, validation rules, errors
internal/db/                 → connection pool, migrations
internal/config/             → environment-based configuration
```

## Database Schema

| Table | Purpose |
|-------|---------|
| `wallets` | Stores wallet ID and balance (BIGINT, cents). `CHECK (balance >= 0)` prevents overdraft at DB level. |
| `transfers` | Records each transfer with a `UNIQUE(idempotency_key)` constraint. Status: `PENDING`, `PROCESSED`, `FAILED`. |
| `ledger_entries` | Double-entry bookkeeping: every transfer creates exactly one `DEBIT` and one `CREDIT` entry. |

## Idempotency Strategy

1. `idempotency_key` on `transfers` has a **UNIQUE constraint**.
2. On each request, we check for an existing transfer with the same key **inside the transaction**.
3. If found → return the existing result (no side effects).
4. If a concurrent duplicate insert causes a unique violation, we catch it and return the existing transfer.

## Concurrency Strategy

- **Pessimistic locking** via `SELECT ... FOR UPDATE` on both wallets, ordered by wallet ID.
- Deterministic ordering prevents deadlocks.
- Balance check + debit/credit happen atomically within a single transaction.
- The `CHECK (balance >= 0)` constraint on `wallets` provides a database-level safety net.

## How to Run

### Prerequisites
- Docker & Docker Compose

### Start everything
```bash
cd wallet-service
docker compose up --build -d
```

### Run locally (with Docker PostgreSQL)
```bash
# Start PostgreSQL only
make db-up

# Wait a few seconds for PG to initialize, then run the app
make run
```

The service will be available at `http://localhost:8080`.

### API Endpoints

```bash
# Create a transfer
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'

# Get wallet balance
curl http://localhost:8080/wallets/wallet_1

# Health check
curl http://localhost:8080/health
```

## How to Test

```bash
# Start PostgreSQL
make db-up

# Run integration tests
make test
```

Or manually:
```bash
DATABASE_URL="postgres://wallet_user:wallet_pass@localhost:5432/wallet_db?sslmode=disable" go test ./tests/... -v -count=1 -race
```

## Test Coverage

| Test | What it validates |
|------|-------------------|
| `TestTransfer_HappyPath` | Successful transfer, correct balances, 2 ledger entries |
| `TestTransfer_IdempotentReplay` | Same key → same result, no duplicate entries |
| `TestTransfer_InsufficientFunds` | Transfer fails, balances unchanged |
| `TestTransfer_MissingIdempotencyKey` | 400 on missing key |
| `TestTransfer_ZeroAmount` | 400 on zero amount |
| `TestTransfer_NegativeAmount` | 400 on negative amount |
| `TestTransfer_SelfTransfer` | 400 on self-transfer |
| `TestTransfer_NonExistentWallet` | 404 on invalid wallet |
| `TestLedger_DebitsEqualCredits` | Sum(DEBIT) = Sum(CREDIT) after multiple transfers |
| `TestTransfer_ConcurrentDebitsFromSameWallet` | No double-spend under concurrent load |
| `TestTransfer_ConcurrentIdempotency` | Same key sent concurrently → exactly one transfer |

## Tradeoffs

| Decision | Rationale |
|----------|-----------|
| Go stdlib (no framework) | Minimal dependencies, clear control flow |
| Pessimistic locking | Better for high-contention wallet debits than optimistic retry loops |
| Amounts as BIGINT cents | Avoids floating-point precision issues |
| Integration tests | More valuable than unit tests for financial correctness verification |
| Single-transaction atomicity | Transfer resolves immediately, no async workflow needed |
