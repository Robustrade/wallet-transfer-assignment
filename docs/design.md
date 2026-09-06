# Wallet Transfer Service — Design Document

## Problem

A wallet-to-wallet transfer API that must be idempotent, atomic, and
concurrency-safe. Every successful transfer must produce exactly two
double-entry ledger entries, and wallet balances must never go negative.

---

## API

```
POST /transfers
{
  "idempotencyKey": "abc123",
  "fromWalletId":  "uuid",
  "toWalletId":    "uuid",
  "amount":        100.00
}

201 Created
{ "transferId": "uuid", "status": "PROCESSED" }

409 Conflict (insufficient funds)
{ "error": { "code": "INSUFFICIENT_FUNDS", "message": "..." } }

409 Conflict (idempotency key reused with different params)
{ "error": { "code": "IDEMPOTENCY_CONFLICT", "message": "..." } }
```

Replays return the original status code verbatim: a 201 on first call
returns 201 on retry. This keeps retries byte-identical to originals and
simplifies client logic.

---

## Database

### wallets
Stores current balance as `NUMERIC(20,2)` with a `CHECK (balance >= 0)`
constraint. Balance is maintained as a stored value updated within the
same transaction as the transfer; it is never derived by summing the
ledger on the read path.

**Why stored balance vs ledger sum?**
- `SUM(ledger_entries)` over a high-volume wallet requires scanning many
  rows, becomes expensive, and creates contention as the ledger grows.
- A stored balance makes the balance check a single row read under lock.
- The ledger still exists for full audit / reconciliation; it just isn't
  the primary source of truth for the current balance.

### transfers
Records the intent and outcome of each transfer attempt. Contains
`from_wallet_id`, `to_wallet_id`, `amount`, `status`, and
`failure_reason`. A CHECK constraint limits status to the three
allowed values. A trigger enforces that PROCESSED and FAILED are
terminal (see State Machine).

### ledger_entries
One DEBIT and one CREDIT row per processed transfer, both inserted
atomically in the same transaction. A `UNIQUE(transfer_id, entry_type)`
constraint makes it physically impossible to insert a duplicate DEBIT or
CREDIT for the same transfer, even under concurrency or retries.

### idempotency_records
Binds an `idempotency_key` to exactly one logical request via a `UNIQUE`
constraint on `idempotency_key`. Stores the `request_hash` (SHA-256 of
the request parameters), final `response_status`, and `response_body`
(raw JSON) so retries can be replayed without re-executing.

---

## Idempotency

1. On every `POST /transfers`, compute `request_hash = sha256(from|to|amount)`.
2. Attempt `INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING RETURNING id`.
3. **Unique violation** → another request already owns this key:
   - Same `request_hash` + terminal status → replay stored response verbatim.
   - Same `request_hash` + `IN_PROGRESS` → 409 (concurrent duplicate).
   - Different `request_hash` → 409 `IDEMPOTENCY_CONFLICT`.
4. **No conflict** → we own the key; proceed to execute the transfer and
   update the record to COMPLETED/FAILED **inside the same DB transaction**.

**Crash-after-commit safety**: because the idempotency record is updated
in the same transaction that commits the transfer, a server crash between
commit and HTTP response leaves a durable COMPLETED record. The client's
retry finds that record and replays the stored response without any
re-execution. This satisfies the "exactly-once at the API level"
requirement.

**Why this matters for different-parameter conflicts**: if the same key
could be reused with different parameters, a client retrying a timed-out
request could silently trigger a completely different transfer. The
`request_hash` check catches this and returns 409 instead.

---

## Concurrency

All wallet mutations happen inside a single database transaction with
row-level locks:

```sql
SELECT id, balance
FROM wallets
WHERE id IN ($1, $2)
ORDER BY id        -- deterministic lock order
FOR UPDATE
```

**Deterministic lock order** prevents deadlocks between concurrent
`A→B` and `B→A` transfers. Both acquire the lock on the wallet with the
lexicographically lower UUID first. Without this ordering, A holds lock₁
and waits for lock₂ while B holds lock₂ and waits for lock₁ — a
classic deadlock. With sorted acquisition, one of them always wins lock₁
and proceeds; the other blocks until the first commits, then runs.

Disjoint transfers (A→B, C→D with no wallet overlap) lock different rows
and run fully in parallel with no serialization overhead.

---

## Transaction Boundary

One database transaction per transfer attempt:

```
BEGIN
  INSERT idempotency_records (IN_PROGRESS)        -- claim the key
  SELECT wallets FOR UPDATE (ordered by id)       -- acquire row locks
  INSERT transfers (PENDING)                       -- record intent
  CHECK source balance >= amount                   -- validate
  UPDATE wallets SET balance = balance - amount    -- debit
  UPDATE wallets SET balance = balance + amount    -- credit
  INSERT ledger_entries DEBIT + CREDIT             -- double-entry
  UPDATE transfers SET status = PROCESSED          -- guarded transition
  UPDATE idempotency_records SET status = COMPLETED, response_body = ...
COMMIT
```

If anything fails between BEGIN and COMMIT, Postgres rolls back the
entire transaction. There is no intermediate committed state where a
debit exists without a credit, or a transfer is PROCESSED without ledger
entries, or a balance has changed without a corresponding idempotency
record. Partial failure is structurally impossible at the committed level.

---

## State Machine

```
         ┌─────────────────────────┐
         │         PENDING         │
         └────────┬────────┬───────┘
                  │        │
           success│        │balance check fails
                  ▼        ▼
            PROCESSED    FAILED
             (terminal)  (terminal)
```

Enforced two ways:
1. Application layer: `WHERE status = 'PENDING'` guard on the UPDATE;
   `RowsAffected() != 1` raises `ErrInvalidTransition`.
2. Database layer: a `BEFORE UPDATE` trigger rejects transitions out of
   PROCESSED or FAILED, regardless of what the application sends.

---

## Failure Handling

| Scenario | Outcome |
|---|---|
| Network error before request arrives | No record created; client retries safely |
| Validation failure (bad amount, same wallet) | 400; no DB writes |
| Wallet not found | 404; idempotency record rolled back |
| Insufficient funds | Transfer created as FAILED; idempotency record FAILED; 409 replay on retry |
| DB error mid-transaction | Full rollback; no partial state |
| Server crash after commit, before response | Idempotency record already COMPLETED; retry replays stored response |
| Concurrent duplicate same key+params | Second request blocks on `INSERT UNIQUE`, then replays result |

---

## Consistency

- `wallets.balance >= 0` enforced by a CHECK constraint at the DB level.
- `ledger_entries UNIQUE(transfer_id, entry_type)` ensures at most one
  DEBIT and one CREDIT per transfer at the physical layer.
- Total money across all wallets is conserved: every successful transfer
  decrements one wallet and increments another by the same amount within
  one atomic transaction.
- The ledger can be used for reconciliation: `SUM(amount) WHERE entry_type='DEBIT'`
  should always equal `SUM(amount) WHERE entry_type='CREDIT'` across all
  PROCESSED transfers.

---

## Testing

| Test | What it verifies |
|---|---|
| `TestTransfer_Success` | Happy-path balances, ledger, status |
| `TestTransfer_InsufficientBalance` | Balance unchanged; transfer FAILED |
| `TestTransfer_IdempotentReplay` | Same key → same ID, single transfer, 2 ledger entries |
| `TestTransfer_IdempotencyConflict` | Same key + different params → 409, no side effects |
| `TestTransfer_Validation` | 400/404 for invalid inputs |
| `TestTransfer_Concurrent_SameSource` | Exactly 1 success, 1 failure; final balance 20; no negative |
| `TestTransfer_Concurrent_DisjointWallets` | Both succeed; no unnecessary serialization |
| `TestTransfer_Concurrent_ReverseDirection` | A→B and B→A; no deadlock; money conserved |
| `TestTransfer_StateTransition_NoReprocess` | FAILED transfer replays failure, not re-executed |
| `TestTransfer_ConcurrentStress_InvariantsHold` | 30 goroutines; total money conserved; debit sum = credit sum; no negative balance |
| `TestTransfer_IdempotencyReplay_PostCrashRetry` | Crash-after-commit replay returns same ID |
| `TestLedger_DoubleEntryInvariant` | DEBIT on source, CREDIT on dest, amounts match |
| `TestTransfer_DecimalAmount` | Sub-dollar amounts (99.99) handled without float error |
| Unit: `TestCentsToNumeric/NumericToCents/RoundTrip` | Exact integer money conversion |
| Unit: `TestCanTransition` | State machine logic |

All integration tests run against a real Postgres instance with the
`-race` detector enabled.

---

## Tradeoffs

| Decision | Chosen | Alternative considered |
|---|---|---|
| Money representation | `int64` cents in Go, `NUMERIC(20,2)` in Postgres | `decimal` library — unnecessary dependency for this scale |
| JSON amount parsing | `json.Number` → `NumericToCents` | `float64` — discarded; float arithmetic on money is never acceptable |
| Idempotency storage | Database row with UNIQUE constraint | Redis — adds infrastructure dependency, harder to make crash-safe |
| In-flight duplicate | Return 409 | Poll/wait — simpler; clients should retry after a short backoff anyway |
| Replay status code | Original status code (201 or 409) | Always 200 — chosen behaviour is more transparent |
| Migrations | Hand-rolled runner | goose — saves one dependency for a ~50-line utility |
| ORM | None (`database/sql` + `lib/pq`) | GORM/sqlx — adds complexity with no benefit at this scope |
| Stored balance | Yes | Ledger-derived — too expensive and contention-prone under concurrency |
