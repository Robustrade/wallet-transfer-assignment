# Wallet Transfer Service — Design Note

## 1. Problem & Scope

A service exposing `POST /transfers` that moves funds between two wallets with:

- **Effectively-once** processing via an `idempotencyKey` (at-least-once delivery + idempotent handling).
- **Double-entry** ledger recording (one DEBIT + one CREDIT per transfer).
- **Correct balances under concurrency** — no double-spend, no negative balances.
- **Safe state transitions** under retries and duplicate delivery.

**Non-goals:** auth, multi-currency, async/queue processing — out of scope.

> "Exactly-once" delivery isn't achievable in distributed systems; we deliver **idempotent processing**, so retries are safe.

---

## 2. Stack & Rationale

| Choice | Why |
|---|---|
| **Go** | Strong concurrency primitives; easy to write the parallel double-spend test. |
| **PostgreSQL** | Real row-level locking (`FOR UPDATE`), enum types, CHECK constraints, transactional DDL. |
| **pgx/v5 + pgxpool** | Modern, fast Postgres driver with explicit transaction control. |
| **Raw SQL (no ORM)** | Transaction boundaries and lock acquisition are exactly what's being evaluated — an ORM hides them. |
| **chi / stdlib net/http** | Lightweight routing; no heavy framework needed for one endpoint. |
| **golang-migrate / goose** | Versioned schema migrations. |
| **testcontainers-go** | Integration tests run against a *real* Postgres, so locking and constraints are genuinely exercised — not mocked away. |

---

## 3. Architecture

Clean layered separation; handlers stay thin, persistence never leaks into business logic.

```
cmd/server/main.go          wiring, config, server start
internal/
  domain/                   entities, state machine, domain errors (no DB/HTTP)
  repository/               Postgres queries; transaction + locking lives here
  service/                  transfer orchestration, idempotency logic
  handler/                  HTTP transport, request validation, DTO mapping
db/migrations/              schema.sql migrations
```

- **Handler** — decode/validate request, map to service call, map result/errors to HTTP.
- **Service** — owns the transfer workflow and idempotency decisions; opens/commits the transaction.
- **Repository** — SQL only; exposes lock-aware operations (e.g. `LockWalletsForUpdate`).
- **Domain** — `Wallet`, `Transfer`, `LedgerEntry`, allowed state transitions, typed errors (`ErrWalletNotFound`, `ErrIdempotencyConflict`; insufficient funds is a recorded FAILED result, not an error).

---

## 4. Data Model

Money is stored as **`BIGINT` minor units (e.g. cents)** — never floats. Correctness invariants are pushed into the schema so the database refuses to be wrong even if app code has a bug.

```sql
CREATE TABLE wallets (
  id         TEXT PRIMARY KEY,
  balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),   -- no overdraft, ever
  currency   TEXT   NOT NULL DEFAULT 'USD',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE transfer_status AS ENUM ('PENDING','PROCESSED','FAILED');

CREATE TABLE transfers (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
  to_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
  amount         BIGINT NOT NULL CHECK (amount > 0),
  status         transfer_status NOT NULL DEFAULT 'PENDING',
  failure_reason TEXT,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (from_wallet_id <> to_wallet_id)                       -- no self-transfer
);

CREATE TYPE entry_type AS ENUM ('DEBIT','CREDIT');

CREATE TABLE ledger_entries (
  id          BIGSERIAL PRIMARY KEY,
  transfer_id UUID NOT NULL REFERENCES transfers(id),
  wallet_id   TEXT NOT NULL REFERENCES wallets(id),
  type        entry_type NOT NULL,
  amount      BIGINT NOT NULL CHECK (amount > 0),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ledger_wallet   ON ledger_entries(wallet_id);   -- balance derivation / history
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);

CREATE TABLE idempotency_records (
  idempotency_key TEXT PRIMARY KEY,                              -- unique = dedup guarantee
  request_hash    TEXT NOT NULL,                                 -- detect key reuse w/ different body
  transfer_id     UUID REFERENCES transfers(id),
  status          TEXT NOT NULL,                                 -- IN_PROGRESS | COMPLETED
  response_status INT,
  response_body   JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Double-entry rule:** every **PROCESSED** transfer produces exactly two entries — one `DEBIT` on the source wallet, one `CREDIT` on the destination — and they must sum to zero. A `FAILED` transfer moved no money, so it has zero entries. Enforced in the service layer; optionally backstopped by a deferred trigger asserting `SUM(DEBIT) = SUM(CREDIT)` per `transfer_id`.

**Balance strategy:** stored balance on `wallets` (O(1) reads, updated under the row lock) **plus** the ledger as the source of truth (`SUM(credits) − SUM(debits)` per wallet). A reconciliation test asserts the two agree, catching drift.

---

## 5. Transfer State Machine

```
PENDING ──> PROCESSED   (funds sufficient: money moved, 2 ledger entries)
PENDING ──> FAILED      (insufficient funds: recorded, money NOT moved, 0 ledger entries)
```

Every *accepted* transfer is inserted as `PENDING` and reaches a terminal state in the same transaction. Pre-acceptance validation (bad amount, self-transfer, unknown wallet) is rejected before any row exists and never enters the machine. Infrastructure errors roll the whole transaction back (nothing persists); `FAILED` is reserved for recorded *business* outcomes.

Transitions are **guarded at the SQL level** so a retry can never re-apply them:

```sql
UPDATE transfers SET status='PROCESSED', updated_at=now()
WHERE id = $1 AND status = 'PENDING';
-- failure branch:
UPDATE transfers SET status='FAILED', failure_reason=$2, updated_at=now()
WHERE id = $1 AND status = 'PENDING';
```

If the guarded `UPDATE` affects 0 rows, the transfer was already moved out of `PENDING` — the operation is a no-op, which is exactly the retry-safe behavior we want.

---

## 6. Idempotency Strategy

**Mechanism: INSERT-first against a UNIQUE key — not check-then-act.**

The naive "does the key exist? if yes return cached, else process" has a race: two concurrent identical requests both read "absent" and both process. Instead we let the `PRIMARY KEY` on `idempotency_key` arbitrate:

1. `INSERT INTO idempotency_records (key, request_hash, status='IN_PROGRESS')`.
2. On **unique violation**: the key is taken. `SELECT ... FOR UPDATE` the existing row.
   - `request_hash` differs → **409/422 conflict** (same key, different payload — never return the wrong cached result).
   - row is `COMPLETED` → **replay the stored response** verbatim.
3. On success: continue with the transfer, then finalize the record to `COMPLETED` with the response.

Because everything runs in **one transaction** (Section 7), a concurrent duplicate blocks on the unique index until the first transaction commits, then sees the committed `COMPLETED` row and replays it. The DB does the serialization for us.

**Single-transaction vs two-phase (a real tradeoff to be able to defend):**

- **Single transaction (chosen):** simplest, strongest atomicity. Business outcomes (including `FAILED`) commit together with their idempotency record, so a duplicate replays the same result. Only *infrastructure* errors roll back — and because the idempotency claim rolls back with them, the key stays free for a safe retry.
- **Two-phase:** record `IN_PROGRESS` in its own transaction first, then do the work. Also lets you return a clean 409 for in-flight duplicates. Cost: more moving parts and crash-recovery handling for stuck `IN_PROGRESS` rows.

I default to single-transaction for clarity and correctness, and call out the two-phase option as the production evolution.

---

## 7. Concurrency, Locking & Transaction Boundary

**One atomic transaction** wraps the entire operation. Either all of it commits or none:

```
BEGIN (READ COMMITTED)
  claim idempotency key (insert)
  lock both wallets  -> SELECT ... FOR UPDATE, in deterministic order
  validate: wallets exist, sufficient balance
  insert transfer (PENDING)
  debit source / credit destination  (UPDATE wallets ...)
  insert two ledger entries (DEBIT, CREDIT)
  guarded transition transfer -> PROCESSED
  finalize idempotency record -> COMPLETED + response
COMMIT
```

**Lock strategy — `SELECT ... FOR UPDATE` with deterministic ordering.** Concurrent debits on the same wallet are serialized by the row lock, so no double-spend is possible. To avoid the classic deadlock (A locks w1→w2 while B locks w2→w1), wallets are **always locked in sorted order of `wallet_id`**, regardless of transfer direction.

**Isolation level — READ COMMITTED.** Sufficient here because `FOR UPDATE` row locks provide the serialization we need on the contended rows, and it avoids the serialization-failure retries that REPEATABLE READ / SERIALIZABLE would introduce.

**Alternative considered — optimistic locking:** a `version` column on `wallets` with `UPDATE ... WHERE version = $expected` under SERIALIZABLE, retrying on conflict. Valid, and better under very low contention, but it pushes a retry loop into the app and adds complexity. Pessimistic `FOR UPDATE` is the better fit for a hot-wallet workload and is easier to reason about. (Documented so the choice is explicit, not accidental.)

---

## 8. Failure Modes

| Condition | Behavior | HTTP |
|---|---|---|
| Same key, same payload, completed | Replay original stored response | original (200/201) |
| Same key, **different** payload | Reject — idempotency conflict | 422 |
| Same key, request still in flight | Blocks then replays (single-txn design) | original |
| `amount <= 0` | Validation error, nothing persisted | 400 |
| `fromWalletId == toWalletId` | Rejected (CHECK + app guard) | 400 |
| Unknown wallet | Validation error | 404 |
| Insufficient funds | Recorded as `FAILED` (no money moved, 0 ledger entries); response cached | 422 |
| DB/unexpected error | Roll back, nothing persisted, key free to retry | 500 |

*Insufficient funds note:* this is the canonical `PENDING → FAILED` transition. The `FAILED` transfer and its idempotency record commit together, so a duplicate replays the same `422` (same key → same answer). Retrying after topping up means using a **new** idempotency key — a new intent. Infrastructure errors, by contrast, roll back entirely and leave the key free.

---

## 9. API Contract

`POST /transfers`

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

Success `201`:

```json
{ "transferId": "…", "status": "PROCESSED", "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2", "amount": 100 }
```

A duplicate with the same key + payload returns the identical body and status.

---

## 10. Testing Strategy

Behavioral tests against a **real Postgres** via testcontainers-go, following Red → Green → Refactor.

- **Happy path** — transfer succeeds; balances and two ledger entries are correct.
- **Idempotency** — same key twice produces exactly one transfer and identical responses.
- **Idempotency conflict** — same key, different payload → rejected.
- **Insufficient funds** — transfer recorded as `FAILED`, no balance change, no ledger entries; duplicate replays the same 422.
- **Ledger correctness / reconciliation** — derived balance == stored balance.
- **Concurrency (the headline test)** — N goroutines fire transfers against the same source wallet in parallel; assert final balance is exactly correct, never negative, and no double-spend occurred.

---

## 11. Observability

- Structured JSON logging with a request/correlation ID propagated to `transfer_id`.
- Log key lifecycle points: idempotency hit/miss, lock acquisition, commit, failure reason.
- (Optional) counters for transfers by status and a transfer-latency histogram.

---

## 12. Assumptions & Tradeoffs

- Wallets are pre-seeded; wallet creation is out of scope.
- Single currency; no FX.
- Synchronous processing (no queue) — acceptable at this scope.
- Pessimistic locking over optimistic — chosen for clarity and hot-wallet contention.
- Single-transaction idempotency over two-phase — chosen for atomicity; business outcomes (incl. `FAILED`) are committed and cached, only infrastructure errors roll back.

---

## 13. AI Usage Note (to complete in the PR)

- **Tool used:** _e.g. Claude_.
- **How I use it:** _design discussion, schema review, test scaffolding — not blind generation._
- **Transcript/prompts:** _attach the session export or list prompts in the repo._
