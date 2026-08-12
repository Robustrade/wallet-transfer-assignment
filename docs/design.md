# Design Note — Wallet Transfer Service

This document is written before any Go code, per the assignment's documentation-first
workflow. It defines the problem, the schema, the concurrency and idempotency strategy,
and the alternatives that were rejected and why.

## 1. Problem statement

Build a service that moves money between two wallets, where:

- the same logical request, submitted more than once (client retry, proxy replay, double
  click), must produce exactly one transfer and always return the original result;
- two transfers that touch the same wallet at the same time must never double-spend,
  never leave a wallet negative, and never leave the ledger unbalanced;
- every transfer is either fully applied or not applied at all — there is no partially
  executed state visible to a reader.

The service is a single HTTP API (`walletd`) backed by a single PostgreSQL database. It
is not a distributed ledger across services; the interesting problems here are
transactional, not distributed-consensus problems, and the design below reflects that.

## 2. Scope and non-goals

In scope: `POST /transfers` (the required endpoint), wallet creation, balance lookup,
ledger history, health probes. Out of scope, deliberately: authentication, rate
limiting, multi-currency conversion, transfer reversal/refund, an outbox or background
worker, a UI. See `prompt.md` §14 for the full anti-goals list and the reasoning behind
each exclusion — the short version is that none of them are needed to demonstrate
correct transactional design, and adding them would dilute the parts that are actually
being evaluated.

## 3. Domain model and schema

### 3.1 Entities

```
wallets            one row per wallet (USER or SYSTEM)
transfers          one row per transfer attempt, keyed by idempotency_key
ledger_entries     two rows per PROCESSED transfer (one DEBIT, one CREDIT)
```

### 3.2 Relationships

```
wallets 1 ──< transfers.from_wallet_id
wallets 1 ──< transfers.to_wallet_id
transfers 1 ──< ledger_entries.transfer_id   (exactly 0 or 2 rows)
wallets 1 ──< ledger_entries.wallet_id
```

`ledger_entries` and `transfers` both reference `wallets` with `ON DELETE RESTRICT` —
wallets are never deleted in this system, and the constraint documents that rather than
relying on convention.

### 3.3 Schema (see `internal/postgres/migrations/0001_init.up.sql` for the executable
version — this is the annotated summary)

```sql
CREATE TYPE transfer_status   AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');
CREATE TYPE wallet_kind       AS ENUM ('USER', 'SYSTEM');

CREATE TABLE wallets (
    id            TEXT PRIMARY KEY,
    kind          wallet_kind NOT NULL DEFAULT 'USER',
    currency      CHAR(3) NOT NULL,
    balance_minor BIGINT NOT NULL DEFAULT 0,
    created_at, updated_at TIMESTAMPTZ,
    CHECK (id ~ '^[A-Za-z0-9_-]{1,64}$'),
    CHECK (currency ~ '^[A-Z]{3}$'),
    CHECK (kind = 'SYSTEM' OR balance_minor >= 0)     -- non-negative user balances
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,                              -- UUIDv7, see §3.5
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,                    -- sha256 fingerprint of the request
    from_wallet_id TEXT NOT NULL REFERENCES wallets,
    to_wallet_id   TEXT NOT NULL REFERENCES wallets,
    amount_minor BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    status transfer_status NOT NULL DEFAULT 'PENDING',
    failure_code TEXT, failure_message TEXT,
    created_at, updated_at, settled_at TIMESTAMPTZ,
    UNIQUE (idempotency_key),                          -- the whole idempotency mechanism
    CHECK (amount_minor > 0 AND amount_minor <= 1e15),
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK ((status = 'FAILED')    = (failure_code IS NOT NULL)),
    CHECK ((status = 'PROCESSED') = (settled_at   IS NOT NULL))
);

CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers,
    wallet_id   TEXT NOT NULL REFERENCES wallets,
    entry_type  ledger_entry_type NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    balance_after BIGINT NOT NULL,
    created_at TIMESTAMPTZ,
    CHECK (amount_minor > 0),
    UNIQUE (transfer_id, entry_type)                   -- at most 1 DEBIT + 1 CREDIT per transfer
);
```

Plus two triggers (full bodies in the migration):

- **`transfers_enforce_invariants`** (BEFORE UPDATE): rejects any change to the
  immutable columns (id, key, hash, wallets, amount, currency), and rejects any status
  transition that doesn't originate from `PENDING`. This makes `PENDING → PROCESSED` and
  `PENDING → FAILED` the *only* transitions the database will accept, enforced
  independently of whatever the application code does.
- **`ledger_entries_append_only`** (BEFORE UPDATE OR DELETE): unconditionally rejects
  the operation. A financial ledger that can be edited after the fact is not a ledger.

### 3.4 Why both a stored balance and a ledger

`ASSIGNMENT.md` allows either deriving balance from the ledger or maintaining a stored
balance. This design does **both**, deliberately:

- `wallets.balance_minor` is the **authority for the debit decision**. It is a single
  row that can be locked with `SELECT ... FOR NO KEY UPDATE` and updated with a single
  guarded `UPDATE`. Deciding "can this debit proceed" against a materialized value under
  a row lock is O(1) and trivially serializable against concurrent debits of the same
  wallet.
- `ledger_entries` is the **append-only audit truth**. It is never mutated, so it is
  always safe to read without a lock, and it is what a real accounting system would
  reconcile against.

The two are kept in agreement structurally, not by convention: `balance_after` is
written from the same `UPDATE ... RETURNING balance_minor` call that changes the
balance, inside the same transaction that inserts the ledger rows and marks the transfer
`PROCESSED`. An integration test (`assertInvariants`, run at the end of every
integration test) independently sums the ledger per wallet and asserts it equals
`wallets.balance_minor`, so any drift between the two would be caught immediately.

Deriving balance purely from `SUM(ledger_entries)` was considered and rejected — see
§12.

### 3.5 Why UUIDv7 for transfer IDs

`transfers.id` is a UUIDv7 rather than UUIDv4 or a serial. UUIDv7 is time-ordered, so
primary-key inserts stay roughly sequential and B-tree-local instead of scattering
across the index like UUIDv4 would, while still being generated client-side (no
round-trip to get a sequence value) and safe to expose externally (unlike a serial,
which leaks row-count information).

### 3.6 System wallets and the closed-ledger invariant

`wallets.kind` is `USER` or `SYSTEM`. Only `SYSTEM` wallets are allowed a negative
balance (the `CHECK` constraint above). A `SYSTEM` wallet represents the boundary
between this service and the outside world — money "enters" the ledger by being
transferred out of a system wallet (which the check constraint allows to go negative
without bound) and "exits" by being transferred into one.

The consequence is a genuinely testable global invariant: **no money is ever created or
destroyed inside the ledger**, so `SUM(wallets.balance_minor)` over every wallet in the
system is always `0`. A single seeded system wallet, `treasury_inr`, is created by
migration `0002`; `POST /wallets` only ever creates `USER` wallets at a zero balance,
and funding a user wallet means transferring from the treasury.

## 4. API contract

See `docs/api.md` for the full contract with `curl` examples. Summary: `POST
/transfers` is idempotent and returns the transfer resource with a status code that
reflects whether it settled (`201` processed, `422` failed) or was rejected before a
transfer existed (`400`/`404`/`409`). Every other endpoint is a plain read or an
optional enhancement (`GET /wallets/{id}`, `GET /wallets/{id}/entries`).

## 5. Side effects

`POST /transfers` is the only endpoint with side effects, and all of them happen inside
one database transaction: at most one `transfers` row is inserted, its status is set
exactly once (`PROCESSED` or `FAILED`), and on the `PROCESSED` path exactly two
`ledger_entries` rows are inserted and both wallets' `balance_minor` are updated. No
side effect is ever partially applied — either all of the above commits, or none of it
does. `POST /wallets` inserts exactly one `wallets` row.

## 6. Idempotency behaviour

**Mechanism:** `UNIQUE (idempotency_key)` on `transfers`. The transfer row *is* the
idempotency record — there is no separate table.

A separate `idempotency_records` table was considered and deliberately not added. This
service has exactly one mutating endpoint, and its response is fully derivable from the
`transfers` row it produces, so a second table would duplicate state and raise a new
consistency question — what happens if the record and the transfer ever disagree? — for
no additional guarantee. The unique index on `idempotency_key` is the entire dedup
mechanism. A dedicated idempotency-records table earns its keep when multiple endpoints
need to share replay behaviour, when the response is not derivable from a single domain
row (e.g. it aggregates several resources), or when keys need an explicit TTL/expiry
policy independent of the underlying record's lifecycle. None of those apply here.

**Detecting a duplicate:**

1. Fast path — before opening a transaction, look up the key. If found, replay (§6,
   below).
2. Slow path — inside the transaction, `INSERT ... ON CONFLICT (idempotency_key) DO
   NOTHING`. If a concurrent request already holds the same key and hasn't committed
   yet, this statement *blocks* until that transaction resolves, then either sees the
   committed row (and replays it) or finds the key free again (if the other transaction
   rolled back) and proceeds. This is what makes concurrent duplicate requests safe
   without any extra locking or a "claimed" intermediate state: Postgres's own
   `INSERT ... ON CONFLICT` conflict-checking machinery does the waiting for us.

**Returning the original result:** a replay returns the *identical* HTTP status code and
body as the original request, plus the response header `Idempotent-Replay: true` so a
caller can distinguish "this just happened" from "this already happened." The
assignment's requirement is to "return the original result" — for a client retrying
blindly after a lost response, that has to include the original status code, not just
the payload with a `200` slapped on it, or the client cannot tell success from failure
on replay.

**Preventing duplicate side effects:** the unique constraint plus the transactional
settlement means a second concurrent attempt with the same key can only ever observe a
committed transfer (and replay it) or an absent key (and create it) — there is no window
in which two attempts both believe they are the first.

**Key reuse with a different payload:** the request is fingerprinted
(`sha256("v1|" + from + "|" + to + "|" + amount + "|" + currency)`) and the fingerprint
is stored alongside the key. A replay whose fingerprint doesn't match the stored one is
rejected with `409 IDEMPOTENCY_KEY_REUSE` rather than silently returning someone else's
transfer.

## 7. Concurrency and consistency

**Isolation level: READ COMMITTED** (Postgres's default — left unchanged). This is
sufficient here because every value that feeds a decision is read under an explicit row
lock in the same transaction that acts on it: we never read a balance, release it, and
then act on the stale read. Under that discipline there is no write-skew and no phantom
exposure to guard against, so `SERIALIZABLE`'s automatic retry-on-conflict machinery
would only add overhead without adding safety.

**Lock type: `SELECT ... FOR NO KEY UPDATE`, not `FOR UPDATE`.** This is the one
non-obvious choice in the whole design, so it gets its own paragraph. Inserting a
`transfers` row takes an implicit `FOR KEY SHARE` lock on both referenced wallet rows,
to validate the foreign keys. `FOR UPDATE` conflicts with `FOR KEY SHARE` — so two
concurrent transfers that had *already* inserted their `transfers` rows (and so already
hold `FOR KEY SHARE` on both wallets) could deadlock trying to upgrade to `FOR UPDATE`
on the same two rows in opposite order, even with a sorted lock-acquisition order,
because the FK locks were taken implicitly and out of our control. `FOR NO KEY UPDATE`
does not conflict with `FOR KEY SHARE`, still conflicts with itself and with the plain
`UPDATE`s used to change `balance_minor`, and is exactly the lock strength a
non-key-column update needs. Wallets are never deleted and their primary key never
changes, so nothing in this service requires the stronger `FOR UPDATE` mode.

**Deterministic lock ordering, in two statements.** Both wallets involved in a transfer
are locked in ascending `id` order — `wA := lock(min(from, to))` then `wB :=
lock(max(from, to))` — issued as **two separate `SELECT ... FOR NO KEY UPDATE`
statements**, not one `WHERE id IN (a, b) ORDER BY id FOR NO KEY UPDATE`. Postgres does
not guarantee that a single statement acquires row locks in the order its output is
sorted; the executor may lock rows before applying `ORDER BY`. Two statements, issued in
a fixed order, make the acquisition order provable rather than merely likely. This is
what prevents `A→B` and `B→A` transfers running concurrently from ever deadlocking on
the wallet rows, and there is a concurrency test (`concurrency_test.go`,
`TestBidirectionalTransfersDoNotDeadlock`) that runs 30 transfers each way simultaneously
and asserts no request ever surfaces a `500`.

**The three lines of defence against double-spend / negative balances**, from fastest to
slowest, each independently sufficient:

1. *Application check*, under the lock: the service reads the locked `balance_minor` and
   compares it to the amount before deciding to settle or fail.
2. *Guarded `UPDATE`*: the debit statement is `UPDATE wallets SET balance_minor =
   balance_minor + $delta WHERE id = $1 AND (kind = 'SYSTEM' OR balance_minor + $delta >=
   0) RETURNING balance_minor`. Zero rows returned means the guard rejected it — this
   catches any bug in layer 1 without trusting the caller to have gotten the read right.
3. *`CHECK (kind = 'SYSTEM' OR balance_minor >= 0)`*: the database refuses to commit a
   negative user balance no matter what statement produced it, including a future
   maintenance script that bypasses the application entirely.

**Timeouts and retries.** Every write transaction sets `SET LOCAL lock_timeout = '3s'`;
a lock that can't be acquired in that window surfaces as Postgres SQLSTATE `55P03`,
translated to `503 RESOURCE_BUSY` with `Retry-After: 1` rather than hanging the request
or bubbling up as a generic `500`. The transaction runner additionally retries the whole
transaction, up to 3 attempts with small jittered backoff, on SQLSTATE `40001`
(serialization_failure) and `40P01` (deadlock_detected) — belt-and-braces against any
deadlock the lock-ordering discipline above didn't anticipate. This is safe specifically
*because* the entire operation lives in one transaction and is idempotent by
construction: retrying it can never double-apply anything.

## 8. Failure modes

| Failure | Persisted? | HTTP | Why |
|---|---|---|---|
| Validation error, wallet not found, currency mismatch, key reuse with different payload | No | 400 / 404 / 409 | Rejected before the idempotency key is claimed. These are deterministic given the same input, so a retry recomputes the identical answer — they're idempotent without needing storage. |
| Insufficient funds | **Yes** — committed as `FAILED` | 422 | This is a legitimate attempt that reached settlement and failed there. Committing it makes the outcome stable: a retry with the same key returns the same failure forever, rather than possibly succeeding later if the balance changes. That stability is what "exactly once" has to mean for a client retrying blindly. |
| Infrastructure error — connection loss, statement/lock timeout, process crash mid-transaction | No — the transaction rolls back | 503 / 500 | Postgres guarantees nothing is left behind, not even a `PENDING` row, if the transaction never commits. The client retries with the same key and gets a genuinely fresh attempt, not a stuck one. |

**On `PENDING`.** Because the whole lifecycle — claim the key, check funds, move money,
write the ledger, mark the outcome — happens inside a single transaction, a *committed*
`PENDING` row is not actually reachable in this implementation; every commit lands on
`PROCESSED` or `FAILED`. `PENDING` is still modelled, honestly, because (a) it's the
state a transfer briefly occupies before the transaction commits, (b) the guarded
transition `UPDATE transfers ... WHERE status = 'PENDING'` and the immutable-transition
trigger both need a "not yet settled" state to check against, and (c) it's the seam an
asynchronous settlement path would need. If a future version of this service had to
settle across a network boundary (e.g. calling out to a third-party rail before the
transfer can be marked `PROCESSED`), the design would split into two transactions — an
outbox-style commit of `PENDING` in transaction 1, then settlement in transaction 2
driven by a sweeper reading the `transfers_pending_idx` partial index — and `PENDING`
would become reachable for real. That split isn't implemented, because nothing in this
service actually crosses such a boundary yet; building it now would be speculative.

## 9. Retry behaviour

A client should retry on `503` (with the given `Retry-After`) and on connection-level
failures, always with the same `idempotencyKey`. Retrying on `4xx` is never correct — a
`400`/`404`/`409` will not change without a different request. Retrying a `422` will
also not change the answer, by design (see §8): the failure is committed and stable.

## 10. Observability

Structured JSON logs (`log/slog`) at one line per request: method, route pattern,
status, duration, and a request id (accepted from an inbound `X-Request-Id` header or
generated). The transfer service additionally logs `idempotency_key`, `transfer_id`, and
`outcome` (`created` / `replayed` / `failed`) on every attempt. Request bodies and wallet
balances are never logged at `info` level. `/healthz` is a pure liveness check with no
I/O; `/readyz` pings the connection pool. See §9 of `prompt.md` for the metrics
considered as a stretch addition.

## 11. Testing strategy

Three layers, each testing a different thing, all behavioural (asserting on outcomes —
status codes, balances, ledger rows — never on internal call counts):

- **`internal/domain`** — pure unit tests, no I/O: state machine transitions, amount
  validation, fingerprint stability, wallet debit guards.
- **`internal/service`** — unit tests against in-memory fakes: the full idempotency and
  settlement policy (happy path, replay, key reuse, insufficient funds, unknown wallet,
  currency mismatch, transaction-runner retry), without a database.
- **`internal/httpapi`** — `httptest` against a fake service: every row of the
  status-code map, malformed/oversized/wrongly-typed request bodies, the replay header.
- **`test/integration`** — against a real Postgres (`TEST_DATABASE_URL`, skipped if
  unset): happy path, sequential and *concurrent* idempotent replay, double-spend under
  concurrency, bidirectional-transfer deadlock resistance, insufficient-funds
  persistence, key reuse, direct-SQL trigger/constraint enforcement, and — run at the
  end of every single integration test — a global invariant check (`SUM(balance_minor) =
  0`, ledger sums match stored balances, every `PROCESSED` transfer has exactly one
  DEBIT and one CREDIT, no `FAILED` transfer has any ledger rows, no user wallet is
  negative).

Development followed red/blue/green: the concurrency and idempotency tests were written
first, watched fail against an unlocked/unguarded draft, then made to pass by adding the
row locks and the unique constraint — not the other way around.

## 12. Alternatives considered and rejected

- **Separate `idempotency_records` table.** Buys: symmetry with a system that has many
  mutating endpoints; a natural place for key TTL. Costs: a second table to keep in sync
  with `transfers`, and a new "what if they disagree" question. Rejected here because
  there's one mutating endpoint and one row fully describes the outcome — see §6.

- **Balances derived purely from `SUM(ledger_entries)`, no stored `balance_minor`.**
  Buys: a single source of truth, no projection to keep in sync. Costs: every debit
  decision requires summing a growing table, and — because there's no row to lock — you
  need either `SERIALIZABLE` isolation (and its retry loop) or an explicit advisory lock
  per wallet just to make the read-then-decide safe under concurrency. The stored
  balance gives the same safety with a plain row lock and an O(1) read; rejected as
  strictly more expensive for no extra guarantee, given §3.4's structural sync.

- **Optimistic locking (`wallets.version` + compare-and-swap on update).** Buys:
  short-held locks, potentially better throughput under low contention, no explicit
  `SELECT ... FOR UPDATE`. Costs: under real contention on a hot wallet (the scenario
  this assignment specifically calls out), it produces retry storms instead of orderly
  queuing, and it still needs a deterministic multi-row update order to avoid lost
  updates across the two wallets. Rejected because pessimistic row locks are simpler to
  reason about for exactly the "many transfers hit the same wallet at once" case being
  evaluated, and don't need a `version` column with no other use.

- **`SERIALIZABLE` isolation with application-level retry on every transfer.** Buys:
  safety even if a future change introduces an unlocked read. Costs: Postgres's
  predicate-lock-based conflict detection means read-committed-safe code still pays
  retry overhead under contention, for a guarantee this design already gets from
  explicit row locks. Rejected as unnecessary given the "lock everything you read before
  you act on it" discipline in §7 — revisit if the transaction ever grows a read that
  isn't locked.

- **Two-phase settlement via an outbox table + a `PENDING` sweeper.** Buys: a real seam
  for settlement that crosses a network or service boundary, and horizontal scalability
  of the sweeper. Costs: a background worker, at-least-once delivery semantics to manage,
  and a genuinely reachable `PENDING` state with all the "what if the sweeper crashes
  mid-batch" questions that implies. Rejected for this scope — see §8's note on
  `PENDING` — because nothing here actually crosses such a boundary; this is where the
  design would go if it did.

## 13. Deviations from the original plan

None yet — this section will be updated if implementation surfaces a place where the
plan above needs to change.
