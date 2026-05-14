# Wallet Transfer Service

A small, production-quality Go service for wallet-to-wallet transfers with idempotency, double-entry ledger, and safe concurrent execution.

---

## Quick start

```bash
# 1. Start PostgreSQL
make up

# 2. Apply schema
make db/migrate

# 3. Run the server
make run
```

The server listens on `:8080` by default.  
Override with `PORT=9000 make run`.

---

## API

### Create wallet

```
POST /wallets
```

```json
{ "id": "wallet_1", "initialBalance": 1000 }
```

`id` is optional — a UUID is generated when omitted.  
`initialBalance` is in the **smallest currency unit** (e.g. cents).

---

### Get wallet

```
GET /wallets/{walletId}
```

---

### Create transfer

```
POST /transfers
```

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId":  "wallet_1",
  "toWalletId":    "wallet_2",
  "amount":        200
}
```

| Field            | Required | Description                              |
|------------------|----------|------------------------------------------|
| `idempotencyKey` | ✅       | Client-supplied unique key               |
| `fromWalletId`   | ✅       | Source wallet                            |
| `toWalletId`     | ✅       | Destination wallet                       |
| `amount`         | ✅       | Positive integer (smallest currency unit)|

Sending the same `idempotencyKey` multiple times returns the original transfer without moving funds again.

**HTTP status codes**

| Situation            | Code  |
|----------------------|-------|
| New transfer created | `201` |
| Insufficient funds   | `422` |
| Wallet not found     | `404` |
| Bad request          | `400` |
| Server error         | `500` |

---

### Transfer history

```
GET /wallets/{walletId}/transfers
```

Returns all transfers involving the wallet, newest first.

---

## Architecture

```
cmd/server/main.go          ← wires everything, starts HTTP server

internal/
  domain/                   ← models (Wallet, Transfer, LedgerEntry) + errors
  db/                       ← DBTX interface, pgxpool factory
  repository/               ← PostgreSQL queries (wallet, transfer, ledger)
  service/                  ← business logic, transaction orchestration
  handler/                  ← HTTP transport, request validation
```

Responsibilities are strictly separated:

- **Handler** — decode request, validate inputs, map errors to HTTP status codes.
- **Service** — business rules, idempotency check, transaction lifecycle.
- **Repository** — SQL queries only; no business logic.
- **Domain** — shared types and sentinel errors.

---

## Database design

### Tables

```sql
wallets         (id, balance, created_at)
transfers       (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
ledger_entries  (id, transfer_id, wallet_id, type, amount, created_at)
```

### Key design decisions

| Decision | Rationale |
|----------|-----------|
| `balance BIGINT CHECK (balance >= 0)` | Database-enforced floor prevents any path from going negative |
| `UNIQUE(idempotency_key)` on transfers | Deduplication is enforced by the DB, not application logic alone |
| `amount > 0` constraint | Zero or negative transfers are rejected at the DB layer |
| `status CHECK IN (PENDING, PROCESSED, FAILED)` | Enforces valid state machine at the DB level |
| `ledger_entries.transfer_id REFERENCES transfers(id)` | Ledger entries cannot be orphaned |
| Indexes on `from_wallet_id`, `to_wallet_id`, `wallet_id` | Efficient history queries |

---

## Idempotency strategy

Every transfer request includes a client-supplied `idempotencyKey`.

**How it works:**

1. The service attempts `INSERT INTO transfers … ON CONFLICT (idempotency_key) DO NOTHING`.
2. If the INSERT returns a row → new transfer, proceed to process it.
3. If the INSERT returns nothing (conflict) → fetch the existing transfer by `idempotency_key` and return it immediately, without performing any side effects.

This guarantees exactly-once semantics at the API level:
- Safe across process restarts (the record is committed before processing begins).
- Safe under concurrent duplicate requests (the UNIQUE constraint serialises them at the DB level).
- Returns the original result for all subsequent calls.

**Known limitation:** If the server crashes after inserting the PENDING record but before completing the transaction, the transfer will be stuck in `PENDING`. A retry with the same key returns `PENDING` rather than re-attempting. A production system would add a background reconciliation job to re-drive stuck PENDING transfers.

---

## Concurrency strategy

Two concurrent debits from the same wallet are the key race condition to prevent.

**Approach: row-level locking (`SELECT … FOR UPDATE`)**

Inside the transfer transaction:

1. Both wallet rows are locked with `SELECT … FOR UPDATE`.
2. Locks are acquired in **sorted wallet ID order** to prevent deadlocks: if transfer A locks `wallet_1` then `wallet_2`, and transfer B does the same order, they cannot deadlock.
3. The balance check happens *after* holding the lock, so no concurrent debit can race between the read and the write.
4. All changes (balance updates, ledger entries, status transition) are committed atomically.

This approach is straightforward, correct, and appropriate for a write-heavy financial service where the cost of a conflict (a brief wait) is acceptable.

**Why not optimistic locking?** Optimistic locking (version columns + retry) adds complexity and can cause unbounded retries under high contention. Pessimistic locking with a short critical section is simpler and predictable.

---

## Transfer state machine

```
        ┌──────────┐
  start │          │
───────►│ PENDING  │
        │          │
        └────┬─────┘
             │
       ┌─────┴──────┐
       ▼            ▼
 ┌──────────┐  ┌─────────┐
 │PROCESSED │  │ FAILED  │
 └──────────┘  └─────────┘
```

- `PENDING → PROCESSED`: balance updates, ledger entries, and status change are committed in one atomic transaction.
- `PENDING → FAILED`: the processing transaction failed (insufficient funds, wallet not found, DB error). The failure reason is recorded.
- Both `PROCESSED` and `FAILED` are terminal states.

---

## Testing

```bash
# Unit tests only (no DB required)
make test/unit

# All tests including integration (requires running PostgreSQL)
make db/migrate/test
make test
```

### Test coverage

| Test | Type |
|------|------|
| Successful transfer, balances move correctly | Integration |
| Duplicate idempotency key returns same transfer, money moves once | Integration |
| Insufficient funds → FAILED transfer | Integration |
| Source wallet not found → FAILED transfer | Integration |
| Same wallet debit/credit rejected | Integration |
| Zero amount rejected | Integration |
| Ledger contains exactly 2 entries per transfer | Integration |
| 10 concurrent debits — no double spend, ledger balances | Integration |
| Handler: valid request → 201 | Unit |
| Handler: missing idempotency key → 400 | Unit |
| Handler: invalid JSON → 400 | Unit |
| Handler: insufficient funds → 422 | Unit |
| Handler: wallet not found → 404 | Unit |

---

## Environment variables

| Variable       | Default                                                   | Description       |
|----------------|-----------------------------------------------------------|-------------------|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable` | PostgreSQL DSN |
| `PORT`         | `8080`                                                    | HTTP listen port  |
