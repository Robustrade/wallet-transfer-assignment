# Wallet Transfer Service — Approach & Design

> This document is the pre-implementation design note, raised as a draft PR before writing code.

---

## Tech Stack

| Concern | Choice | Reason |
|---------|--------|--------|
| Language | TypeScript (Node.js) | Strong typing, great ecosystem for HTTP services |
| HTTP framework | Express | Mature, minimal, widely understood |
| Database | PostgreSQL | ACID transactions, row-level locking, CHECK constraints |
| DB client | `pg` (node-postgres) | Raw SQL gives full control over locking/transactions |
| Testing | Jest + Supertest | Behavior-level integration tests against real DB |
| Validation | Zod | Parse-don't-validate for request bodies |

---

## Schema Design

```
wallets
  id           TEXT PRIMARY KEY
  balance      NUMERIC(19,4) NOT NULL DEFAULT 0
  created_at   TIMESTAMPTZ

  CONSTRAINT balance_non_negative CHECK (balance >= 0)

transfers
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid()
  idempotency_key  TEXT NOT NULL UNIQUE          ← deduplication anchor
  from_wallet_id   TEXT REFERENCES wallets(id)
  to_wallet_id     TEXT REFERENCES wallets(id)
  amount           NUMERIC(19,4) NOT NULL
  status           TEXT DEFAULT 'PENDING'        ← PENDING | PROCESSED | FAILED
  failure_reason   TEXT
  created_at / updated_at TIMESTAMPTZ

  CONSTRAINT amount_positive CHECK (amount > 0)
  CONSTRAINT status_valid CHECK (status IN ('PENDING','PROCESSED','FAILED'))
  CONSTRAINT different_wallets CHECK (from_wallet_id != to_wallet_id)

ledger_entries
  id           UUID PRIMARY KEY
  wallet_id    TEXT REFERENCES wallets(id)
  transfer_id  UUID REFERENCES transfers(id)
  type         TEXT  ← DEBIT | CREDIT
  amount       NUMERIC(19,4)
  created_at   TIMESTAMPTZ

  CONSTRAINT type_valid CHECK (type IN ('DEBIT','CREDIT'))
  CONSTRAINT amount_positive CHECK (amount > 0)
```

Indexes: `transfers(idempotency_key)` (unique), `ledger_entries(wallet_id)`, `ledger_entries(transfer_id)`.

---

## Idempotency Strategy

- `idempotency_key` is stored with a **UNIQUE constraint** on the `transfers` table.
- On every `POST /transfers`, the service first checks for an existing record with that key.
- If found → return the original result immediately (no DB side effects).
- If two concurrent requests carry the same key, only one INSERT succeeds; the other gets a PostgreSQL `23505` (unique violation) and falls back to fetching the already-committed record.
- This guarantees **exactly-once semantics** even across process restarts, because the record is durable from the moment the first request commits.

---

## Concurrency Strategy

The transfer execution follows this sequence inside a single DB transaction:

1. **Lock both wallets** atomically using `SELECT ... FOR UPDATE`, ordered by wallet ID (alphabetically) to eliminate the deadlock class caused by lock-order inversion.
2. **Check balance** under the lock (no phantom reads possible after `FOR UPDATE`).
3. **Insert transfer** record as `PENDING`.
4. **Insert ledger entries** (DEBIT + CREDIT) in the same transaction.
5. **Update wallet balances** (`balance - amount` / `balance + amount`).
6. **Mark transfer `PROCESSED`**.
7. **Commit** — all or nothing.

The `balance >= 0` CHECK constraint on `wallets` acts as a last-resort guard against double-spending even if application logic has a bug.

---

## Transfer State Machine

```
             ┌─────────┐
  POST /     │ PENDING │
  transfers  └────┬────┘
                  │
       ┌──────────┴──────────┐
       ▼                     ▼
  PROCESSED               FAILED
  (success)          (insufficient balance
                      or unexpected error)
```

State transitions are safe under retries:
- A duplicate request finding a `PROCESSED` record returns it unchanged.
- A duplicate request finding a `FAILED` record also returns it unchanged (same idempotency key = same result).

---

## Layer Structure

```
src/
  handler/          ← HTTP: parse request, call service, serialize response
  service/          ← Business logic: idempotency, orchestration, state machine
  repository/       ← Persistence: SQL queries, no business logic
  domain/           ← Types, error classes, state definitions
  db/               ← Pool setup, migration runner
```

---

## API Surface

```
POST /transfers              Create a transfer
GET  /wallets/:id            Get wallet balance (optional, aids testing)
GET  /wallets/:id/ledger     Get ledger entries for a wallet (optional)
```

---

## Testing Plan

| Test | Coverage goal |
|------|--------------|
| `transfer.service.test.ts` | Happy path, insufficient funds, wallet not found |
| `idempotency.test.ts` | Duplicate key → same result, no double side-effects |
| `concurrency.test.ts` | Parallel debits on same wallet, correct final balance |
| `transfer.handler.test.ts` | Request validation, HTTP status codes |

Tests run against a real PostgreSQL instance (Docker Compose provided). Each test suite migrates and seeds its own schema to remain isolated.

---

## Tradeoffs / Assumptions

- **NUMERIC(19,4)** for money — avoids floating-point errors. Amounts are treated as fixed-point.
- **Balance stored, not derived** — faster reads, consistent snapshot even if ledger is queried separately. Integrity guaranteed by DB constraints + transactional writes.
- **No retry queue for PENDING transfers** — a crashed process leaves a transfer in `PENDING`. A production system would have a background sweeper; out of scope here.
- **Single-region, single-DB** — no distributed transaction complexity needed.
- **Idempotency key scoped to transfers only** — client is responsible for generating unique keys per intent.
