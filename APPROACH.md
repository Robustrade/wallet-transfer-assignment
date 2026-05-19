# Wallet Transfer Service — Approach

## Understanding of the Problem

Build a wallet-to-wallet transfer service that is:

- **Idempotent** — the same request (identified by `idempotencyKey`) must never execute twice, even under retries or duplicate delivery
- **Consistent** — every transfer produces exactly two balanced ledger entries; balances must never drift
- **Concurrency-safe** — concurrent debits on the same wallet must not cause double-spending or negative balances
- **Auditable** — transfer state transitions are persisted and queryable

---

## Schema Design

### Prerequisites

The schema uses `gen_random_uuid()` from Postgres' `pgcrypto` extension.
The first migration must enable it:

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

### `wallets`

```sql
CREATE TABLE wallets (
  id          TEXT        PRIMARY KEY,
  name        TEXT        NOT NULL,
  balance     NUMERIC(18,2) NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`balance` is a cached running total updated inside the same transaction as every ledger write.
This gives O(1) balance reads. Correctness is verifiable at any time by comparing
`SUM(CASE WHEN type='CREDIT' THEN amount ELSE -amount END) WHERE wallet_id = X`
against `wallets.balance`. A plain `SUM(amount)` would overstate the balance because
both DEBIT and CREDIT amounts are stored as positive numbers — direction is encoded in `type`.

### `transfers`

```sql
CREATE TABLE transfers (
  id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  idempotency_key  TEXT        UNIQUE NOT NULL,
  from_wallet_id   TEXT        NOT NULL REFERENCES wallets(id),
  to_wallet_id     TEXT        NOT NULL REFERENCES wallets(id),
  amount           NUMERIC(18,2) NOT NULL CHECK (amount > 0),
  status           TEXT        NOT NULL DEFAULT 'PENDING'
                               CHECK (status IN ('PENDING','PROCESSED','FAILED')),
  failure_reason   TEXT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- updated_at is not maintained by a DB trigger; the application sets it explicitly
-- on every status transition (PENDING → PROCESSED / PENDING → FAILED).
```

`idempotency_key` has a `UNIQUE` constraint directly on the transfers table.
This is the database-level guarantee that no two transfers share the same key.

### `ledger_entries`

```sql
CREATE TABLE ledger_entries (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  transfer_id  UUID        NOT NULL REFERENCES transfers(id),
  wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
  type         TEXT        NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
  amount       NUMERIC(18,2) NOT NULL CHECK (amount > 0),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT unique_entry_per_transfer_type UNIQUE (transfer_id, type)
);
```

Every transfer produces exactly two rows: one DEBIT from the source wallet, one CREDIT to the destination wallet.
Amount is always stored as a positive number; the `type` column encodes direction.

**Enforcing exactly two entries** requires two complementary guarantees:

- *No more than two per transfer:* the `UNIQUE(transfer_id, type)` constraint enforces this at the database level —
  it is impossible to insert a second DEBIT or CREDIT for the same transfer.
- *No fewer than two per transfer:* both ledger inserts and the transfer's transition to `PROCESSED`
  happen inside the same Phase 2 transaction. Either both rows commit together with the status
  update, or the entire transaction rolls back and the transfer remains in `PENDING`.
  A `PROCESSED` transfer therefore always has exactly two ledger entries.

A periodic reconciliation query (`SELECT COUNT(*) FROM ledger_entries GROUP BY transfer_id HAVING COUNT(*) <> 2`)
restricted to PROCESSED transfers should always return zero rows.

### Why no separate `idempotency_records` table

The `UNIQUE` constraint on `transfers.idempotency_key` is sufficient on its own.
A separate idempotency table would add a second table and a foreign-key relationship
without buying any guarantee that the column-level unique constraint does not already provide.

The idempotency key has exactly the same lifecycle as the transfer it identifies —
created with it, kept forever with it, never updated. Co-locating the key on the
transfer row keeps the schema minimal and lets a single DB constraint enforce uniqueness.

---

## Idempotency Strategy

### How the key is stored

`idempotency_key` is a column on the `transfers` table with a `UNIQUE` constraint.

### How duplicate requests are detected — three layers

**Layer 1 — Fast path (no lock):**
Before any transaction, query for an existing transfer with the same key.
If found, return the cached result immediately without touching the DB further.

**Layer 2 — Guarded INSERT:**
Inside the transaction, attempt `INSERT INTO transfers (idempotency_key, ...)`.
If the key already exists, Postgres raises a unique violation.
The handler catches this, fetches the existing transfer, and returns it.

**Layer 3 — Unique constraint:**
Two concurrent requests with the same key can both pass Layer 1 (neither has written yet).
Both attempt the INSERT. Postgres serialises them at the constraint: exactly one succeeds,
the other receives a constraint violation and falls through to return the existing record.

Layer 1 reduces unnecessary DB round-trips. Layer 3 is the correctness guarantee.

### How duplicate side effects are prevented — two-phase execution

**The tradeoff:** wrapping the idempotency-key INSERT and all business writes in a single
transaction would be simpler, but a process crash mid-execution rolls back the PENDING row
along with everything else — leaving no trace of an in-flight transfer. Future retries would
have no way to distinguish "never attempted" from "attempted, status unknown".

To get a crash-visible PENDING record while still keeping business writes atomic, execution
is split into two committed transactions:

**Phase 1 — claim the key:**
```sql
BEGIN;
  -- Lock BOTH wallet rows in sorted id order first. The INSERT below performs
  -- foreign-key checks on from_wallet_id and to_wallet_id, and Postgres takes
  -- an implicit row-share lock on each referenced wallet row in column
  -- (from, then to) evaluation order — NOT sorted order. For an opposing
  -- transfer (B -> A) that order is reversed, and it can interleave with a
  -- concurrent Phase 2 FOR UPDATE to form a deadlock cycle
  -- (observed: SQLSTATE 40P01 at the INSERT). Pre-locking the two wallet rows
  -- in the same global sorted order used by Phase 2 makes every transaction
  -- acquire wallet locks in one consistent order, so no cycle can form. The
  -- existence check here also gives the 404 (with no row written, since the
  -- transaction rolls back) before the key is claimed.
  SELECT id FROM wallets WHERE id = $lower_id  FOR UPDATE;
  SELECT id FROM wallets WHERE id = $higher_id FOR UPDATE;
  INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
  VALUES (gen_random_uuid(), $1, $2, $3, $4, 'PENDING');
  -- unique constraint on idempotency_key fires here if duplicate
COMMIT;
```
The PENDING row is committed immediately. The idempotency key is now owned.
If the process crashes after Phase 1, a PENDING row is visible to a reconciliation job.
A duplicate request arriving now hits the unique constraint, fetches the existing row, and returns its current state (see below).

> **Lock-ordering invariant:** *every* transaction that touches the two wallet
> rows — Phase 1 (FK-induced locks, made deterministic by the pre-locks above)
> and Phase 2 (`FOR UPDATE`) — acquires them in the same sorted (lower id
> first) order and holds them until its own commit. Ordered locking across all
> transactions makes the wait-for graph acyclic, so the system is deadlock-free
> even under opposing concurrent transfers.

**Phase 2 — execute business logic:**
```sql
BEGIN;
  -- application sorts the two wallet IDs and locks them with two separate
  -- statements in deterministic order. A single IN(...) + ORDER BY query
  -- does not guarantee lock acquisition order in Postgres (the executor
  -- may scan and lock rows in plan-dependent order before sorting), so
  -- two explicit statements are required.
  SELECT * FROM wallets WHERE id = $lower_id  FOR UPDATE;
  SELECT * FROM wallets WHERE id = $higher_id FOR UPDATE;
  -- check balance
  -- if insufficient:
  --   UPDATE transfers SET status = 'FAILED', failure_reason = $1, updated_at = now() WHERE id = $2;
  -- if sufficient:
  --   INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount) VALUES (..., 'DEBIT', ...);
  --   INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount) VALUES (..., 'CREDIT', ...);
  --   UPDATE wallets SET balance = balance - $amount WHERE id = $from_id;
  --   UPDATE wallets SET balance = balance + $amount WHERE id = $to_id;
  --   UPDATE transfers SET status = 'PROCESSED', updated_at = now() WHERE id = $id;
COMMIT;
```
If Phase 2 crashes, the PENDING row remains and business side effects are fully rolled back —
no ledger entries, no balance changes. The reconciliation job marks stale PENDING rows FAILED.

### Duplicate requests against an in-flight transfer

A duplicate request can arrive while the original is still in `PENDING` (Phase 2 has not yet committed).
The handler always returns the current persisted state of the transfer; it does not block, retry,
or wait for Phase 2 to finish.

| Original transfer state when duplicate arrives | Response |
|---|---|
| PENDING | `200 OK` with `status: PENDING` — client should poll `GET /transfers/:id` until terminal |
| PROCESSED | `200 OK` with `status: PROCESSED` — final response |
| FAILED | `200 OK` with `status: FAILED` and `failure_reason` — final response |

This keeps the idempotency contract simple — duplicates never trigger side effects, regardless of timing —
and pushes the polling responsibility to the client, which is the only party that knows how long it is willing to wait.
The API contract example below shows the `PROCESSED` case because that is the steady-state response;
in-flight responses contain the same shape with `status: PENDING`.

---

## Concurrency Strategy

### Approach: pessimistic row-level locking (`SELECT FOR UPDATE`)

Transfer execution runs in two committed transactions (see Idempotency section):

**Phase 1** — lock both wallet rows `FOR UPDATE` in sorted id order, then
INSERT transfer(PENDING) and commit (claims the idempotency key). The pre-lock
is required because the FK checks in the INSERT take implicit row-share locks
on both wallet rows in unsorted (column) order; pre-locking in sorted order
keeps the global lock ordering consistent with Phase 2 (see Idempotency →
Lock-ordering invariant). It also yields a no-DB-write 404 when a wallet is
missing, since the transaction rolls back before the row is inserted.

**Phase 2** — business logic transaction:
1. Sort the two wallet IDs lexicographically (lower ID first)
2. Lock the lower-ID wallet: `SELECT * FROM wallets WHERE id = $lower FOR UPDATE`
3. Lock the higher-ID wallet: `SELECT * FROM wallets WHERE id = $higher FOR UPDATE`
   *(Two separate statements — a single `IN (...) ORDER BY id FOR UPDATE` does
   not guarantee lock acquisition order in Postgres.)*
4. Read balances from the locked rows
5. Check that `from_wallet` has sufficient funds
6. If sufficient: INSERT ledger entries × 2, UPDATE balances × 2, UPDATE transfer → PROCESSED
7. If insufficient: UPDATE transfer → FAILED
8. Commit

### Why lock ordering prevents deadlocks

Without consistent ordering, two concurrent transfers in opposite directions deadlock:

```
T1: wallet_A → wallet_B  — locks A, waits for B
T2: wallet_B → wallet_A  — locks B, waits for A
→ deadlock cycle
```

Postgres has a deadlock detector that breaks the cycle by aborting one of the
transactions with a `40P01` (deadlock_detected) error — the loser must retry.
The system stays live, but retries waste work, add latency, and complicate the
application code with retry loops.

Sorting wallet IDs before locking means both transactions always acquire locks in the same order,
so the second one simply waits for the first to commit. The deadlock cycle never forms,
no transaction is aborted, and no retry logic is needed.

### Why pessimistic locking over optimistic locking

Optimistic locking (version columns + retry on conflict) introduces retry loops in the application layer.
Under high contention, retries can cascade, making latency unpredictable.
Pessimistic locking with consistent ordering is more explicit and gives predictable,
deadlock-free behaviour with no retry complexity — the right default for a financial system
where correctness matters more than throughput.

---

## Transfer State Machine

```
PENDING → PROCESSED   (funds available, ledger written, balances updated)
PENDING → FAILED      (insufficient funds)
```

**Why PENDING exists:**

The transfer row is inserted with `status = PENDING` before business logic executes.
This means:

- If the process crashes mid-execution, a PENDING row is left behind as evidence
- A background reconciliation job can detect stale PENDING transfers and mark them FAILED
- The idempotency_key is claimed the moment the row is inserted, preventing any concurrent
  duplicate from creating a second row even if the first is still in PENDING

**State transitions are terminal:**
A PROCESSED or FAILED transfer cannot transition to any other state.
Reversal is a separate transfer, not a state change.

---

## API Contract

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

`amount` is a JSON number, decoded via `json.Number` (not `float64`) to avoid precision loss.
Parsed into exact decimal arithmetic on the server before any computation.

**Response (201 Created on new transfer, 200 OK on duplicate key):**
```json
{
  "id": "uuid",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "createdAt": "2026-05-18T10:00:00Z"
}
```

Response `amount` mirrors the request format — a JSON number — for symmetry.

**Error responses:**
- `400` — malformed request (missing fields, non-positive amount, same source and destination)
- `404` — wallet not found

**Insufficient funds is NOT an error response.** It returns `201 Created` (or `200 OK` on duplicate)
with a transfer resource whose `status` is `FAILED` and `failure_reason` is `"insufficient funds"`.
Rationale: the transfer was successfully created, persisted, and audited — the request itself
was valid. Only the business outcome was unfavourable. Treating it as a 4xx would conflict with
the audit-trail design (FAILED rows are deliberately committed) and would force retries to
re-discover the same outcome via a different response shape.

Example FAILED response:
```json
{
  "id": "uuid",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "FAILED",
  "failureReason": "insufficient funds",
  "createdAt": "2026-05-18T10:00:00Z"
}
```

**Duplicate key behaviour:**
Any request reusing an existing `idempotencyKey` returns `200 OK` with the original transfer
(in its current state — PENDING, PROCESSED, or FAILED). The request body of the duplicate
is not validated against the original; the key alone identifies the transfer. This matches
the assignment's contract ("return the original result") and avoids the overhead of storing
or hashing request bodies for comparison.

### GET /wallets/:id/balance *(optional)*

Returns current balance derived from cached `wallets.balance`.

### GET /transfers/:id *(optional)*

Returns a single transfer with its ledger entries.

---

## Failure Modes and Handling

| Scenario | Behaviour |
|---|---|
| Insufficient funds | Phase 1 commits PENDING; Phase 2 transitions to FAILED, no ledger entries written, COMMIT |
| Same source and destination | Rejected at validation, no DB write |
| Non-positive amount | Rejected at validation, no DB write |
| Duplicate idempotency key | Return existing transfer in its current state; no second transfer created. Body of duplicate is not compared against the original. |
| Process crash mid-transfer | PENDING row left; balance unchanged (no ledger written yet); safe to retry |
| Wallet not found | 404, no DB write |

**Why COMMIT on failure (insufficient funds):**
A FAILED transfer row is committed, not rolled back.
This preserves the evidence that the attempt happened, which is essential for auditability.
The audit trail must be complete even for failed attempts.

---

## Testing Strategy

### Unit tests

- Service layer: transfer execution, idempotency check logic, state transitions
- Domain validation: negative amounts, same-wallet transfers, missing fields

### Integration tests (against real Postgres)

- Happy path: transfer executes, two ledger entries created, balances updated correctly
- Insufficient funds: transfer marked FAILED, no ledger entries, balances unchanged
- Idempotency: same key sent twice returns identical response, only one transfer row in DB
- Idempotency mid-flight: a duplicate request arriving while the original is still PENDING returns the PENDING transfer without triggering Phase 2 a second time
- Ledger balance invariant: `SUM(ledger_entries.amount WHERE type=CREDIT) = SUM(... WHERE type=DEBIT)` across all entries for a transfer
- Ledger ↔ balance consistency: `SUM(CASE WHEN type='CREDIT' THEN amount ELSE -amount END) per wallet` matches `wallets.balance`

### Concurrency test

- N goroutines concurrently transferring between the same two wallets
- Assert: no negative balances, money is conserved (total balance across wallets unchanged), ledger matches balance
- Same idempotency key sent by M goroutines concurrently
- Assert: exactly one transfer row exists, all M responses return the same transfer ID

---

## Assumptions and Trade-offs

| Decision | Rationale |
|---|---|
| `idempotency_key` on `transfers` table, not a separate table | Atomicity — key is claimed and transfer is created in one INSERT. Separate table needs two writes with its own race. |
| Cached balance on `wallets` | O(1) reads; correctness verifiable by comparing to ledger SUM at any time |
| Pessimistic locking | Predictable latency under contention; no retry loops |
| Lock ordering by wallet ID | Prevents deadlocks without needing serializable isolation |
| Amount as JSON number, decoded via `json.Number` | Matches the assignment's request example; `json.Number` preserves precision (unlike `float64`) before conversion to decimal |
| FAILED transfers are committed (not rolled back) | Audit trail must include failed attempts |
| No authentication | Out of scope for this assignment |
| No reversal API | Not in functional requirements; can be added as a subsequent transfer if needed |

---

## Implementation Plan

1. Schema + migrations
2. Domain models and error sentinels
3. Repository layer (wallet reads with FOR UPDATE, transfer INSERT, ledger INSERT, balance UPDATE)
4. Service layer (idempotency check, lock ordering, business logic, state transitions)
5. Handler layer (request parsing, validation, error mapping)
6. Integration tests
7. Concurrency test
