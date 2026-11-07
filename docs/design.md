# Wallet Transfer Service: Design Note

Status: **Draft v1**. Written before implementation, as the docs-first workflow in `ASSIGNMENT.md` asks.
Later commits update this note whenever the implementation changes a decision.

---

## 1. Problem statement

Move money between two wallets so that:

- a transfer is **atomic**: balances, ledger entries and transfer state change together or not at all;
- a request with an `idempotencyKey` executes **at most once** and every retry gets the **original result**
  (exactly-once at the API level);
- every successful transfer is recorded as a **balanced double-entry** pair;
- balances stay **correct under concurrent requests**, so a wallet can never be spent twice.

Out of scope for v1: authentication, multi-currency, fees, reversals, and an external payment rail.

## 2. Key decisions (summary)

| Concern | Decision | Main reason |
|---|---|---|
| Language / DB | Go 1.24, PostgreSQL 16, `pgx/v5` | Matches the CI pipeline. Postgres row locks and constraints carry the correctness guarantees. |
| Money | `BIGINT` minor units (cents), never floats | Exact arithmetic. |
| Balance model | **Stored balance** on `wallets`, updated in the same transaction as the ledger | O(1) reads and a lockable row to serialise debits on. The ledger stays the audit source and is reconciled against the stored balance. |
| Concurrency | `READ COMMITTED` + **pessimistic row locks** (`SELECT … FOR NO KEY UPDATE`) on both wallets, taken **first** in the transaction and **always in ascending wallet-id order** | No read-then-write race and no retry loops. A single global lock order means no deadlocks (§6). |
| Idempotency | `transfers.idempotency_key UNIQUE`, claimed with `INSERT … ON CONFLICT DO NOTHING`. A duplicate is detected by comparing the stored columns with the request, with no hash. | The database, not application code, decides who wins a duplicate race. The result is durable across restarts. |
| Transaction shape | **One DB transaction per transfer** | There is no external side effect, so a single transaction removes partial execution entirely. |
| Insufficient funds | Persisted as a `FAILED` transfer (no ledger rows) and replayed for that key | Retrying the same key must not succeed later just because the wallet was topped up in between. |

The rest of the document explains each decision and the alternatives considered.

## 3. API contract

### `POST /transfers`

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Validation is done in the handler and domain layers. **Nothing is persisted** when validation fails:

| Field | Rule |
|---|---|
| `idempotencyKey` | optional; if present, 1–255 chars |
| `fromWalletId`, `toWalletId` | required, non-empty, and different from each other |
| `amount` | required, integer, `> 0` (minor units) |
| body | valid JSON, unknown fields rejected |

Responses (body is the transfer resource):

```json
{
  "id": "6f1c…",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-11-05T10:00:00Z"
}
```

| Situation | HTTP | Body | Persisted? |
|---|---|---|---|
| Transfer succeeded | `201 Created` | transfer, `status: PROCESSED` | yes |
| Insufficient funds | `422 Unprocessable Entity` | transfer, `status: FAILED`, `failureReason: INSUFFICIENT_FUNDS` | yes, as `FAILED` |
| Replay of a known key with the same payload | **same status and body as the original** + header `Idempotent-Replayed: true` | original transfer | no new rows |
| Known key reused with a **different** payload | `409 Conflict` | error `IDEMPOTENCY_KEY_REUSED` | no |
| Malformed or invalid request | `400 Bad Request` | error `VALIDATION_ERROR` | no |
| Source or destination wallet does not exist | `404 Not Found` | error `WALLET_NOT_FOUND` | no (see §9) |
| Lock or statement timeout, DB unavailable | `503 Service Unavailable` | error `RETRYABLE` | no (rolled back), so it is safe to retry with the same key |
| Unexpected error | `500` | generic error, details only in logs | no (rolled back) |

Error body: `{"error": {"code": "IDEMPOTENCY_KEY_REUSED", "message": "..."}}`.

Planned read endpoints (follow-up commits): `GET /wallets/{id}` (balance) and `GET /transfers/{id}`.

## 4. Data model

```sql
CREATE TABLE wallets (
    id             TEXT        PRIMARY KEY,
    balance        BIGINT      NOT NULL DEFAULT 0,
    allow_negative BOOLEAN     NOT NULL DEFAULT FALSE,   -- only for the system funding wallet
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallets_balance_non_negative CHECK (allow_negative OR balance >= 0)
);

CREATE TABLE transfers (
    id              UUID        PRIMARY KEY,
    idempotency_key TEXT        UNIQUE,                  -- NULLs are distinct: keyless requests are not deduplicated
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    amount          BIGINT      NOT NULL,
    status          TEXT        NOT NULL,
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_amount_positive   CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets  CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid      CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_failure_reason    CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL)),
    CONSTRAINT transfers_idem_key_length   CHECK (char_length(idempotency_key) BETWEEN 1 AND 255)
);
CREATE INDEX transfers_from_wallet_idx ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_idx   ON transfers (to_wallet_id,   created_at DESC);

CREATE TABLE ledger_entries (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id UUID        NOT NULL REFERENCES transfers(id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets(id),
    entry_type  TEXT        NOT NULL,
    amount      BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entry_type_valid    CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_amount_positive     CHECK (amount > 0),
    CONSTRAINT ledger_one_side_per_transfer UNIQUE (transfer_id, entry_type)
);
CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id);
```

What the constraints guarantee, even if the application code has a bug:

- **No duplicate transfer for a key**: `UNIQUE (idempotency_key)`.
- **No ledger row without a transfer**: `FK transfer_id`.
- **At most one debit and one credit per transfer**: `UNIQUE (transfer_id, entry_type)`.
- **No overdraft**: `CHECK (allow_negative OR balance >= 0)`. This is a backstop to the service-level check, so a missed check turns into a rolled-back transaction instead of a negative balance.
- **No nonsense states**: positive amounts, distinct wallets, a closed status set, and a failure reason present iff the transfer is `FAILED`.

What the constraints do **not** guarantee, and how it is covered instead:

- *Exactly* two entries per `PROCESSED` transfer, with equal amounts that match `transfers.amount` and the right wallets.
  These are written in the same transaction as the status change and covered by tests plus a reconciliation
  query (§12). A `DEFERRABLE INITIALLY DEFERRED` constraint trigger could enforce this at commit time. I left it out
  for v1 to keep the schema readable (see §10).
- Ledger immutability: the repository exposes no update or delete for ledger entries. In production I would also
  `REVOKE UPDATE, DELETE` from the application role.

### Why no separate `idempotency_records` table

The assignment suggests one. I deliberately put the key on `transfers` instead:

- There is exactly one idempotent operation, and its result *is* the transfer row. A separate table would duplicate
  that state and introduce two rows that could disagree.
- The "original result" is rebuilt deterministically from the terminal transfer row, which never changes once it
  reaches `PROCESSED` or `FAILED`. No serialized response blob is needed.
- Payload-mismatch detection compares `from_wallet_id`, `to_wallet_id` and `amount` directly, so no request hash is needed (§5.1).

A dedicated `idempotency_records (key, scope, request_hash, response, expires_at)` table becomes the better choice
when several endpoints share idempotency, when keys must be scoped per client or expire, or when the response
cannot be rebuilt from domain state. The repository interface keeps that change local.

### Funding wallets and the ledger invariant

If wallets were created with a non-zero opening balance, that money would have no ledger entry, and
`balance == Σcredits − Σdebits` would not hold. So every wallet starts at `0`, and money enters the system through
a normal transfer from a **system funding wallet** (`allow_negative = true`). As a result, two invariants always hold:

1. for each wallet: `balance = Σ CREDIT − Σ DEBIT` over its ledger entries;
2. globally: `Σ balance = 0` and `Σ CREDIT = Σ DEBIT`.

Seed data and tests fund wallets this way.

## 5. Transfer workflow and transaction boundary

A keyed request first tries a **lock-free replay fast path** (step 0). Everything after that runs in **one**
`READ COMMITTED` transaction owned by the service layer:

```
0. (no transaction) SELECT * FROM transfers WHERE idempotency_key = $key
     └─ found → compare payload (§5.1) → replay or 409. No wallet locks are taken.
     └─ not found → continue. This read is only an optimisation; step 2 makes the authoritative decision.
BEGIN
1. SELECT id, balance, allow_negative FROM wallets
   WHERE id IN ($from, $to) ORDER BY id FOR NO KEY UPDATE    -- first locks in the tx, ascending id order
     └─ fewer than 2 rows → ROLLBACK; 404 WALLET_NOT_FOUND
2. INSERT INTO transfers (id, idempotency_key, from, to, amount, status='PENDING')
   ON CONFLICT (idempotency_key) DO NOTHING RETURNING id
     └─ no row returned → a concurrent request with this key committed first:
          ROLLBACK; load the existing transfer; compare payload (§5.1) → replay or 409
3. domain decision: wallet.CanDebit(amount)?
     └─ no  → UPDATE transfers SET status='FAILED', failure_reason='INSUFFICIENT_FUNDS'
              WHERE id=$id AND status='PENDING'
              COMMIT → 422
4. UPDATE wallets SET balance = balance - $amount WHERE id = $from
   UPDATE wallets SET balance = balance + $amount WHERE id = $to
5. INSERT ledger_entries (DEBIT, $from), (CREDIT, $to)
6. UPDATE transfers SET status='PROCESSED' WHERE id=$id AND status='PENDING'
     └─ 0 rows affected → invariant violated → ROLLBACK, 500
COMMIT → 201
```

The order of steps 1 and 2 matters. §6 explains the deadlock that the opposite order causes.

### 5.1 Detecting a reused key with a different payload

There is no fingerprint or hash column. Every semantic field of the request (`fromWalletId`, `toWalletId`,
`amount`) is already persisted verbatim on the transfer row, and the key itself is the lookup. The check is a
domain method:

```go
func (t Transfer) MatchesRequest(r TransferRequest) bool {
    return t.FromWalletID == r.FromWalletID && t.ToWalletID == r.ToWalletID && t.Amount == r.Amount
}
```

Match → replay the stored result. Mismatch → `409 IDEMPOTENCY_KEY_REUSED`, and the stored transfer is never
modified. A hash would just be a derived copy of these columns and could not be more precise than comparing them,
and it would add canonicalisation concerns such as field order and number formatting.

A `request_hash` column (SHA-256 of canonical JSON) becomes necessary once the request carries fields that are
**not** persisted as columns, such as a free-form `metadata` object. That is when to add it.

### State machine

```
PENDING ──► PROCESSED
   │
   └──────► FAILED
```

- The domain model (`Transfer.MarkProcessed()`, `Transfer.MarkFailed(reason)`) refuses any transition that does not
  start from `PENDING`. `PROCESSED` and `FAILED` are terminal.
- The repository applies a transition as a **guarded update**, `… WHERE status = 'PENDING'`, and the service treats
  "0 rows affected" as an error. A stale or duplicate actor can therefore never move a terminal transfer.
- **What `PENDING` means here:** because the whole workflow is one transaction, `PENDING` is the state of the
  claimed row *inside* the transaction and is never committed. Other transactions only ever see terminal states.
  This is intentional. With no external side effect, splitting the work into "commit PENDING, then process"
  would only add a partial-execution window (a crash leaves stuck `PENDING` rows that need a recovery worker) and
  buy nothing. The state stays in the model because the two-phase variant becomes necessary as soon as a step
  leaves the database, such as an external payment rail or a cross-service call. §10 sketches that design.

## 6. Concurrency strategy

### Same wallet, different requests (the double-spend case)

Step 1 takes `FOR NO KEY UPDATE` locks on both wallet rows. Any other transfer touching either wallet blocks at step 1
until the first commits, then reads the **committed** balance (in `READ COMMITTED`, each statement takes a new
snapshot after the lock is acquired). The funds check in step 3 is therefore made against a balance that
cannot change before commit, so there is no read-then-write race. The `CHECK (balance >= 0)` constraint is a
second, independent barrier.

### Deadlock analysis

**Rule: every wallet lock a transaction will ever need is taken in its first statement, in ascending id order.**
Every lock acquired later is either on a row the transaction already holds, or on its own new unique-index entry.

Two collisions this rule prevents:

1. **Opposing transfers (A→B and B→A).** Locking "source, then destination" would let T1 hold A and wait for B
   while T2 holds B and waits for A. A single `WHERE id IN (…) ORDER BY id FOR NO KEY UPDATE` statement locks
   in sorted order, because the `LockRows` step runs above the `Sort`, so both transactions queue on the lower id
   first and no cycle can form.

2. **Foreign-key lock upgrade (found during design review).** An earlier draft inserted the transfer *before*
   locking the wallets. Inserting a row with foreign keys makes Postgres run a check that takes `FOR KEY SHARE`
   on each referenced wallet row. `FOR KEY SHARE` locks are compatible with one another, so two concurrent
   transfers that share wallet A both acquire one. Each then requests `FOR UPDATE` on A, which conflicts with
   the other's `FOR KEY SHARE`:

   ```
   T1 (A→B): INSERT transfer → KEY SHARE(A,B) … FOR UPDATE(A) waits for T2
   T2 (A→C): INSERT transfer → KEY SHARE(A,C) … FOR UPDATE(A) waits for T1   ⇒ deadlock
   ```

   This hits *any* two transfers sharing a wallet, including the same-direction hot-wallet case, and sorting the
   locks does not help. Two independent fixes, both applied:
   - **Lock wallets before inserting** (steps 1 → 2). The later FK checks then run on rows this transaction
     already holds exclusively, so they never wait.
   - **Use `FOR NO KEY UPDATE`, not `FOR UPDATE`.** We change only `balance` and never `wallets.id`, so we do not
     need the lock mode that conflicts with `FOR KEY SHARE`. `FOR NO KEY UPDATE` is also the lock the later
     `UPDATE wallets SET balance …` takes anyway, so no lock gets upgraded inside the transaction.

3. **Wallet locks and the idempotency key's index entry.** A transaction waits on another's uncommitted key
   entry (step 2) only *after* it holds all its wallet locks, and the transaction it waits for already holds
   all of its own wallet locks and needs no more wallet locks. The wait graph therefore cannot form a cycle.

Postgres would detect any deadlock and abort one transaction (`40P01`), which maps to `503 RETRYABLE`. That is
a safety net, not the design. A concurrency test with many transfers over overlapping wallets asserts that none
occur.

### Same idempotency key, concurrent requests

Two requests with key `k` both miss the fast path (step 0). Each locks its wallets (step 1), so if they name the
same wallets the second simply waits there. Whichever reaches step 2 second **waits** on the first transaction's
uncommitted unique-index entry:

- first commits → the second gets no row back from `ON CONFLICT`, rolls back, and replays the committed result
  (or returns `409` if its payload differs);
- first rolls back (crash, timeout) → the second's insert succeeds and it performs the transfer.

Either way exactly one transfer exists, and the unique index alone decides the winner. The step-0 read is never
trusted to decide that, because a "not found" there could be stale. It only avoids taking locks on a busy wallet
for retries of transfers that have already finished. That is safe because a terminal transfer never changes.

### Alternatives considered

| Option | Why not chosen |
|---|---|
| `SERIALIZABLE` isolation | Correct, but every conflict surfaces as `40001` and needs an application retry loop. Under a hot wallet that means wasted work and harder-to-explain behaviour. Explicit locks make the serialisation point visible in the code. |
| Optimistic locking (`version` column) | Good under low contention, but degrades into retry storms on hot wallets, which is exactly the double-spend scenario. |
| Single conditional update `UPDATE … SET balance = balance - $1 WHERE id = $2 AND balance >= $1` | Atomic and lock-light, and a reasonable alternative. I chose explicit `FOR NO KEY UPDATE` because it keeps the funds decision in the domain layer (not hidden in SQL), distinguishes "wallet missing" from "insufficient funds" cleanly, and gives one consistent lock order for both rows. |
| Balance derived from the ledger (`SUM`) | No single row to lock, so a locking or serialisation scheme is needed anyway, and reads cost O(entries). The ledger is still kept and reconciled against the stored balance. |
| Advisory locks / Redis locks | An extra moving part with failure modes of its own, when Postgres row locks already do the job transactionally. |

### Timeouts

Each transaction runs with `SET LOCAL lock_timeout` and `statement_timeout` (configurable, default a few seconds)
plus the request `context`. A request that cannot get its locks fails fast with `503 RETRYABLE` and a full
rollback. Retrying with the same key is safe.

## 7. Failure modes and retry behaviour

| Failure | Effect | Client retry with same key |
|---|---|---|
| Crash or network loss **before commit** | Transaction rolled back, nothing persisted, key not consumed | Executes the transfer normally |
| Commit succeeded, **response lost** | Transfer is durable | Gets the original result (`Idempotent-Replayed: true`) |
| Process restart | All idempotency state lives in Postgres | Same as above |
| Duplicate delivery, concurrent | Second request waits on the unique index, then replays | Same single result |
| Insufficient funds | `FAILED` persisted | Gets the same `422 FAILED` result, even after a top-up. A new attempt needs a new key. |
| Wallet not found | Nothing persisted | Same `404`, until the wallet exists (see §9) |
| Lock or statement timeout, or deadlock `40P01` (prevented by design, §6) | Rolled back | Safe to retry |
| Bug that violates an invariant | A DB constraint fires and rolls back | `500`. No corrupt state is committed. |

Keyless requests (`idempotencyKey` omitted) are **not** deduplicated, as the assignment specifies. Retrying them is
the client's responsibility.

## 8. Code structure

```
cmd/server/            main: config, pgx pool, migrations, HTTP server, graceful shutdown
internal/domain/       Transfer, Wallet, LedgerEntry, Status, transitions, validation, domain errors (no I/O)
internal/service/      TransferService: idempotency, workflow, owns the transaction boundary
internal/repository/   interfaces used by the service
internal/repository/postgres/  pgx implementations, SQL, Postgres error → domain error mapping
internal/handler/      HTTP: decode/validate, call the service, map results and errors to status codes
migrations/            versioned SQL (embedded, applied at startup)
```

- The **service** opens the transaction through a small `TxManager.WithinTx(ctx, func(repos Repos) error)`, so
  repositories never begin or commit on their own and never decide workflow.
- The **domain** owns the rules: amount > 0, distinct wallets, `CanDebit`, legal transitions.
- The **handler** has no business logic. It maps transport ↔ domain and error kind ↔ HTTP status.

Libraries: stdlib `net/http` (Go 1.22+ routing patterns), `pgx/v5`, `log/slog`, a migration tool (goose or
golang-migrate), `google/uuid`, `testify`, `testcontainers-go`.

## 9. Assumptions

- Single currency, with amounts in integer minor units.
- Idempotency keys are global, since there is no client or tenant concept, and are kept permanently because they
  live on the transfer row.
- Wallets are created by migrations, seed data or tests. Wallet management APIs are out of scope for v1.
- An unknown wallet is a client error, not a transfer outcome, so it is not persisted. It is detected when the
  locking `SELECT` in step 1 returns fewer than two rows, and the FK remains as a backstop. Consequence: the
  same key retried after that wallet is created will execute. I accept this because no money moved under that key.
- Transfer IDs are UUIDs generated in the service, so they can appear in logs before the insert.

## 10. Tradeoffs and possible hardening

- **Two-phase workflow for external side effects.** Commit `PENDING` first, perform the external call with the
  transfer id as its idempotency key, then transition. Add a sweeper that resolves stale `PENDING` rows and
  possibly an outbox. This is not needed while every effect is inside one Postgres transaction.
- **Deferred constraint trigger** that asserts, at commit, that a `PROCESSED` transfer has exactly one debit and one
  credit matching its amount and wallets. This moves a test-enforced invariant into the database.
- **Hot-wallet throughput.** Row locks serialise all transfers on one wallet. For very hot wallets (such as a
  treasury), options are sub-account sharding or batching. That is outside this scope.
- **Ledger partitioning or archiving** by time, once the ledger grows large.

## 11. Observability

- Structured JSON logs (`log/slog`) for each transfer request with `request_id`, `transfer_id`, `idempotency_key`,
  `from`, `to`, `amount`, `outcome` (`processed|failed|replayed|conflict|error`) and `duration_ms`.
  No balances or PII beyond ids.
- An `X-Request-Id` header is passed through or generated and echoed back.
- Planned (follow-up): Prometheus counters by outcome, a latency histogram, and a lock-timeout counter.
  The reconciliation query from §4 is exposed as a test helper and could run as a periodic job.

## 12. Testing strategy

Tests are written first (red), then the minimal implementation (green), then a refactor.

| Level | What | How |
|---|---|---|
| Domain unit | validation rules, `CanDebit`, legal and illegal state transitions | pure Go, table-driven |
| Service integration (the core) | everything in §5–§7 against **real Postgres**. Locking cannot be meaningfully mocked. | `testcontainers-go` Postgres, or `TEST_DATABASE_URL` if set; schema reset per test |
| Handler | request validation, status-code and error mapping, replay header | `httptest` with a fake service |

Behaviours each test asserts:

- successful transfer: balances move, exactly two balanced ledger entries, `PROCESSED`;
- insufficient funds: `FAILED`, no ledger rows, balances unchanged, replay returns `FAILED` even after a top-up;
- same key twice (sequential): one transfer, identical responses, second one flagged as a replay;
- same key, different payload: `409`, original untouched;
- **same key, N concurrent requests**: exactly one transfer and all N responses identical;
- **N concurrent debits** from a wallet that can afford only k: exactly k `PROCESSED`, final balance ≥ 0 and as
  expected, ledger reconciles;
- **opposing concurrent transfers** A↔B: all complete, no deadlock, totals conserved;
- **many concurrent transfers over overlapping wallets** (same direction and mixed): no `40P01` errors, which is the
  regression test for the FK lock-upgrade deadlock in §6;
- validation failures and unknown wallets persist nothing;
- after every service test, a reconciliation helper asserts both invariants from §4.

CI: `golangci-lint run ./...`, `test -z "$(gofmt -l .)"`, `go test ./... -race -cover`.
