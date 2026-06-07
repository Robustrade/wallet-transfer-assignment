# Wallet Transfer Service — Solution Approach

## Overview

This document outlines my understanding of the problem and the proposed implementation approach for the wallet-to-wallet transfer service.

## Understanding

The goal is to build a reliable transactional service that supports wallet-to-wallet transfers with:
- **Idempotent request handling** — duplicate requests with the same `idempotencyKey` return the original result without re-executing
- **Double-entry ledger** — every transfer produces exactly two ledger entries (debit + credit)
- **Correct balance tracking** — wallet balances are always consistent
- **Concurrency safety** — no double spending under concurrent requests
- **Safe state transitions** — transfers move through PENDING → PROCESSED/FAILED atomically

---

## Technology Choices

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Language | Go 1.24 | CI is already configured for Go |
| Database | PostgreSQL (with SQLite fallback for tests) | Assignment preference; strong transaction/locking support |
| HTTP Framework | `net/http` (stdlib) + chi router | Minimal dependencies, idiomatic Go |
| Testing | Go standard `testing` + `testify` | Behavioral tests with clear assertions |
| Migrations | `golang-migrate` or embedded SQL | Reproducible schema setup |

---

## Proposed Architecture

```
cmd/server/main.go          — application entry point
internal/
  handler/                   — HTTP handlers (thin, validation + serialization)
  service/                   — business logic, orchestration, idempotency
  repository/                — database interactions (transactions, queries)
  domain/                    — entities, value objects, state machine
  middleware/                — logging, request ID
db/
  migrations/                — SQL migration files
```

### Layer Responsibilities

- **Handler**: Parse request, validate input, call service, return response. No business logic.
- **Service**: Idempotency check, transfer workflow (create PENDING → execute → mark PROCESSED/FAILED), orchestrate repository calls within a transaction.
- **Repository**: Raw SQL queries, transaction management, row-level locking.
- **Domain**: Transfer, Wallet, LedgerEntry structs; state machine validation.

---

## Database Schema Design

### `wallets`
```sql
CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency    TEXT NOT NULL DEFAULT 'USD',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
- `balance >= 0` constraint prevents negative balances at the DB level
- Using BIGINT (cents) to avoid floating-point issues

### `transfers`
```sql
CREATE TABLE transfers (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  TEXT NOT NULL UNIQUE,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets(id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    status           TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_transfers_idempotency_key ON transfers(idempotency_key);
```

### `ledger_entries`
```sql
CREATE TABLE ledger_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id  UUID NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    entry_type   TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
```

### No separate `idempotency_records` table
I'm using the `UNIQUE` constraint on `transfers.idempotency_key` directly. The transfer record itself serves as the idempotency record — simpler, fewer tables, same guarantees.

---

## Idempotency Strategy

1. On each `POST /transfers` request, attempt to look up an existing transfer by `idempotency_key`.
2. If found → return the existing transfer result (regardless of status).
3. If not found → proceed with transfer creation inside a transaction.
4. The `UNIQUE` constraint on `idempotency_key` provides a database-level safety net against race conditions between duplicate requests.

This ensures **exactly-once semantics**: even if two identical requests arrive simultaneously, only one will successfully insert.

---

## Concurrency Strategy

**Approach: Pessimistic locking with `SELECT ... FOR UPDATE` ordered by wallet ID.**

1. Begin a serializable transaction.
2. Lock both wallets using `SELECT ... FOR UPDATE` **ordered by wallet ID** (to prevent deadlocks).
3. Check sender balance ≥ transfer amount.
4. Debit sender, credit receiver (UPDATE balance).
5. Insert two ledger entries.
6. Update transfer status to PROCESSED.
7. Commit.

If balance is insufficient → mark transfer as FAILED and commit.

Ordering locks by wallet ID is critical to avoid deadlocks when two concurrent transfers involve the same wallets in opposite directions.

---

## Transfer Execution Flow

```
Request arrives
  ↓
Validate input (handler)
  ↓
Check idempotency_key (service)
  ├── Found → return cached result
  └── Not found → continue
        ↓
      BEGIN TX
        ↓
      INSERT transfer (PENDING)
        ↓
      SELECT wallets FOR UPDATE (ordered by ID)
        ↓
      Check balance ≥ amount
        ├── No → mark FAILED, COMMIT, return error
        └── Yes → continue
              ↓
            UPDATE balances
              ↓
            INSERT ledger entries (DEBIT + CREDIT)
              ↓
            UPDATE transfer → PROCESSED
              ↓
            COMMIT
              ↓
            Return success
```

---

## Testing Strategy

| Category | What's tested |
|----------|---------------|
| Unit tests | Domain logic (state transitions, validation) |
| Integration tests | Full transfer flow against real DB (using testcontainers or SQLite) |
| Idempotency tests | Duplicate requests return same result, no double execution |
| Concurrency tests | Parallel transfers on same wallet, no double spend |
| Failure tests | Insufficient funds, invalid wallets, partial failures |

---

## API Design

### POST /transfers
**Request:**
```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

**Response (201 Created):**
```json
{
  "id": "uuid",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "createdAt": "2026-06-07T..."
}
```

**Response (409 / 200 for duplicate):**
Returns the original transfer result.

### GET /wallets/:id/balance (optional enhancement)
### GET /transfers/:id (optional enhancement)

---

## Assumptions & Tradeoffs

1. **Stored balance over derived balance** — faster reads, slightly more complex writes. The CHECK constraint + transactional updates keep it consistent.
2. **PostgreSQL as primary DB** — the assignment prefers it; I'll use testcontainers for integration tests so no external setup is needed.
3. **No distributed transactions** — single-database system; all consistency is handled within PostgreSQL transactions.
4. **Amount in integer cents** — avoids floating-point precision issues.
5. **No authentication** — not in scope.
6. **Wallet pre-seeding** — wallets are assumed to exist; a seed script or migration will create test wallets.

---

## Implementation Plan

1. ✅ Set up Go module, project structure, dependencies
2. ✅ Database migrations (schema)
3. ✅ Domain models + state machine
4. ✅ Repository layer (with transaction support)
5. ✅ Service layer (transfer logic + idempotency)
6. ✅ Handler layer (HTTP endpoints)
7. ✅ Integration tests (transfer, idempotency, concurrency)
8. ✅ README with run/test instructions
9. ✅ PR with full explanation
