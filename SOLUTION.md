# Wallet Transfer Service — Solution Notes

## How to Run

```bash
# 1. Start Postgres
docker-compose up -d

# 2. Apply migrations (main DB + test DB)
make migrate
docker-compose exec postgres psql -U wallet -d wallet \
  -c "CREATE DATABASE wallet_test OWNER wallet;"
TEST_DATABASE_URL=postgres://wallet:wallet@localhost:5434/wallet_test?sslmode=disable \
  go run ./cmd/migrate -database \
  "postgres://wallet:wallet@localhost:5434/wallet_test?sslmode=disable" -action up

# 3. Start the server
DATABASE_URL=postgres://wallet:wallet@localhost:5434/wallet?sslmode=disable \
  go run ./cmd/server
```

The server listens on `:8080` by default (`SERVER_ADDR` env var overrides this).

## How to Test

```bash
# All tests — domain unit tests + service integration tests (requires Postgres)
TEST_DATABASE_URL=postgres://wallet:wallet@localhost:5434/wallet_test?sslmode=disable \
  go test -v -race -count=1 ./...

# Domain-only unit tests (no database required)
go test -v ./internal/domain/...
```

## Example API calls

```bash
# Transfer funds
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_alice","toWalletId":"wallet_bob","amount":100}' | jq .

# Check wallet balance
curl -s http://localhost:8080/wallets/wallet_alice | jq .

# Replay — returns the original result, no duplicate transfer
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_alice","toWalletId":"wallet_bob","amount":100}' | jq .
```

The seed migration creates three wallets: `wallet_alice` (100 000), `wallet_bob` (50 000), `wallet_carol` (0).

---

## Schema Design

### `wallets`
| Column | Type | Notes |
|--------|------|-------|
| `id` | TEXT PK | Caller-supplied identifier |
| `balance` | BIGINT | Stored in the smallest currency unit (cents). `CHECK (balance >= 0)` is the last line of defence against a negative balance. |
| `version` | BIGINT | Incremented on every UPDATE; used by the repository as an optimistic-concurrency check alongside `SELECT FOR UPDATE`. |

### `transfers`
| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID PK | Server-generated |
| `idempotency_key` | TEXT UNIQUE | The unique constraint is the idempotency lock — no separate table required. |
| `from_wallet_id` / `to_wallet_id` | TEXT FK | Both must reference existing wallets; `CHECK (from ≠ to)` enforced in the DB. |
| `amount` | BIGINT | `CHECK (amount > 0)` enforced in the DB. |
| `status` | ENUM | `PENDING → PROCESSED \| FAILED`. Managed by the domain state machine. |
| `failure_reason` | TEXT nullable | Human-readable reason when status is FAILED. |

### `ledger_entries`
| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID PK | |
| `transfer_id` | UUID FK | Every entry is owned by a transfer. |
| `wallet_id` | TEXT FK | |
| `type` | ENUM | `DEBIT` or `CREDIT`. |
| `amount` | BIGINT | Always positive; direction is encoded by `type`. |

Two entries are created atomically with every PROCESSED transfer — one DEBIT from the source wallet, one CREDIT to the destination wallet.

---

## Idempotency Strategy

**Mechanism**: The `transfers.idempotency_key` column has a `UNIQUE` constraint. The first step inside the service transaction is to `INSERT` the transfer record. If that insert fails with Postgres error `23505` (unique violation), the request is a replay.

**Flow for a duplicate request**:
1. INSERT attempt → unique violation → rollback the open transaction immediately (no wallet locks acquired, no balance reads, no side effects).
2. Fetch and return the original transfer record from the database.
3. The caller receives the same response they would have received the first time, regardless of whether it was `PROCESSED` or `FAILED`.

**Why this is safe across process restarts**: the idempotency key is persisted in the same transaction as the transfer record. If the process dies before the transaction commits, neither the key nor any side effects are stored — the next attempt is treated as a first attempt and the transaction runs again.

---

## Concurrency Strategy

**Row-level locking**: Inside the transfer transaction, both wallet rows are locked with `SELECT … FOR UPDATE` before any balance reads or updates. This prevents two concurrent transactions from reading the same balance and both succeeding with an amount that would leave one wallet negative.

**Deadlock prevention**: The two locks are always acquired in ascending alphabetical order of wallet ID, regardless of which is the source and which is the destination. Two concurrent transfers between the same pair of wallets will therefore always attempt to acquire locks in the same order, making a deadlock impossible.

**Version check**: `UpdateBalance` issues `WHERE version = $current_version`. If the version has changed since we read the wallet (which cannot normally happen given `FOR UPDATE`, but acts as a belt-and-suspenders guard), the update affects zero rows and the service returns an error and the transaction is rolled back.

**DB-level constraint**: The `CHECK (balance >= 0)` constraint on `wallets` is a final, unconditional guard. Even if application logic had a defect, Postgres will refuse to commit a transaction that would produce a negative balance.

---

## Failure Durability

When any step inside the transaction fails (wallet not found, insufficient funds, unexpected DB error), `failTransfer()`:
1. Calls `transfer.MarkFailed(reason)` on the domain object.
2. Persists the `FAILED` status with the reason using `UpdateStatus`.
3. **Commits** that update so the terminal state is durable.

This means every attempted transfer — successful or not — leaves an auditable record in the database.

---

## Architecture

```
cmd/server/main.go          → wires pool → repos → service → handlers
internal/
  domain/                   → pure Go; no DB imports; entities + state machine + errors
  repository/               → SQL; accepts DBTX (pool or tx); no business logic
  service/                  → transaction boundary, idempotency, orchestration
  handler/                  → HTTP; decode → service call → encode; no business logic
  middleware/                → request logging
testutil/                   → shared DB helpers for integration tests
db/migrations/              → numbered up/down SQL files
```

The `DBTX` interface (satisfied by both `*pgxpool.Pool` and `pgx.Tx`) lets repositories work inside or outside a transaction without duplicating code. The service owns the transaction and passes its `Tx` into scoped repository instances.

---

## Tradeoffs and Assumptions

| Decision | Rationale |
|----------|-----------|
| Amounts as `BIGINT` | Avoids floating-point precision issues entirely. Callers are expected to work in the smallest unit (e.g. cents). |
| Stored balance column | Avoids a full ledger scan on every balance read. Kept consistent by updating it inside the same transaction as the ledger entries. |
| Single idempotency table (the transfer row itself) | One fewer table to maintain; the unique constraint on `transfers.idempotency_key` is sufficient. |
| `READ COMMITTED` isolation | Sufficient because `SELECT FOR UPDATE` acquires row-level locks, preventing the read-then-write race for the specific rows we care about. |
| No wallet creation API | Out of scope per the assignment; wallets are seeded in the migration for development and testing. |
