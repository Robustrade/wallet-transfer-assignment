## Summary

Implemented a production-grade, highly reliable, and concurrency-safe **Wallet Transfer Service** in Go and PostgreSQL.

Key capabilities delivered:
- **Exactly-Once API Semantics**: Atomic idempotency record handling storing request hashes and response payloads to safely handle retries and network drops.
- **Double-Entry Ledger Engine**: Atomically generates 1 `DEBIT` entry and 1 `CREDIT` entry per transfer transaction with database uniqueness constraints (`CONSTRAINT uniq_ledger_transfer_type UNIQUE (transfer_id, entry_type)`).
- **Deterministic Deadlock Prevention**: Lexicographically sorted single-query row locking (`SELECT FOR UPDATE WHERE id IN ($1, $2) ORDER BY id ASC`) to eliminate deadlocks during concurrent reciprocal transfers ($W_1 \rightarrow W_2$ and $W_2 \rightarrow W_1$).
- **Minor-Unit Monetary Precision**: Balance and transfer amounts represented in integer cents (`int64`), preventing floating-point precision errors.
- **Layered Clean Architecture**: Strict separation of concerns across Handler $\rightarrow$ Service $\rightarrow$ Repository $\rightarrow$ Domain Models with constructor dependency injection.

---

## AI disclosure

1. **Tool Used**: Antigravity AI Coding Assistant (Google DeepMind / Claude 3.7 Sonnet / Gemini models).
2. **Usage Pattern**:
   - Used for designing the clean layered architecture and database schema constraints.
   - Used for identifying PostgreSQL foreign key locking behavior and formulating single-query lexicographical row locking (`ORDER BY id ASC`).
   - Used for writing table-driven unit tests, HTTP integration tests, and high-concurrency race condition tests (`TestTransferConcurrencySafety`).
3. **Session Transcript**:
   Session trajectory and step-by-step logs are stored in the repository artifacts directory: `<appDataDir>/brain/47cd1d3c-1daf-43af-952d-13b0706d8c3b/`.

---

## Schema Design

- `wallets`: `id` (PK), `balance` (BIGINT CHECK >= 0), `currency` (VARCHAR), `created_at`, `updated_at`.
- `transfers`: `id` (PK), `idempotency_key` (UNIQUE), `from_wallet_id` (FK), `to_wallet_id` (FK), `amount` (BIGINT CHECK > 0), `status` (`PENDING`, `PROCESSED`, `FAILED`), `failure_reason`, `created_at`, `updated_at`.
- `ledger_entries`: `id` (PK), `transfer_id` (FK), `wallet_id` (FK), `entry_type` (`DEBIT`, `CREDIT`), `amount` (BIGINT CHECK > 0), `created_at`, `CONSTRAINT uniq_ledger_transfer_type UNIQUE (transfer_id, entry_type)`.
- `idempotency_records`: `idempotency_key` (PK), `request_hash` (VARCHAR), `status` (`IN_PROGRESS`, `COMPLETED`, `FAILED`), `response_code`, `response_body`, `transfer_id`, `created_at`, `updated_at`.

---

## Idempotency Strategy

1. Incoming requests check `idempotency_records` inside an atomic DB transaction.
2. If `status` is `COMPLETED` or `FAILED`, the stored HTTP response payload is replayed immediately.
3. If `status` is `IN_PROGRESS`, HTTP status `409 Conflict` is returned to prevent duplicate execution.
4. Reusing an existing idempotency key with a different request payload returns HTTP `422 Unprocessable Entity`.
5. On new requests, idempotency record creation, wallet balance updates, transfer creation, double-entry ledger insertion, and idempotency completion execute atomically inside a single ACID database transaction.

---

## Concurrency Strategy

1. **Deterministic Row Locking**: Wallet IDs are lexicographically ordered (`min(W1, W2)` and `max(W1, W2)`) and locked in a single SQL statement (`SELECT FOR UPDATE ORDER BY id ASC`).
2. **Deterministic Foreign Key Order**: Ledger entries are sorted by `WalletID` prior to insertion, guaranteeing uniform foreign key evaluation order across all concurrent transactions.
3. **Database Constraints**: `CHECK (balance >= 0)` serves as a secondary database-level failsafe against negative balances.

---

## How to Run

```bash
docker run -d --name wallet-postgres -p 5432:5432 -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=wallet_db postgres:16-alpine
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable"
go run ./cmd/server
```

---

## How to Test

```bash
go test -v -race -cover -p 1 ./...
```

---

## Tradeoffs / Assumptions

- Monetary amounts are represented as 64-bit signed integers (`int64`) in minor units (cents).
- PostgreSQL is used as the primary production and testing database engine.
- Single-instance API server deployment assuming database-level ACID transactions for cross-node synchronization.

---

## Checklist

- [x] Tests pass (`go test -v -race -cover -p 1 ./...`)
- [x] Lint passes (`golangci-lint run ./...`)
- [x] Format check passes (`gofmt -l .`)
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
