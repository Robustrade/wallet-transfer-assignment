# Wallet Transfer Service: design note

Written before the implementation, per the documentation-first workflow in
[ASSIGNMENT.md](../ASSIGNMENT.md). It records the contract, the failure modes,
and the decisions that are easy to get wrong and expensive to change later.

## Problem

Move money between two wallets such that the transfer is atomic, recorded as a
balanced double-entry pair, safe under concurrent debits of the same wallet, and
**exactly-once at the API level** when the caller supplies an `idempotencyKey`.

The hard part is not the arithmetic. It is duplicate delivery, partial
execution and race conditions: the caller may send the same request twice, the
network may lose a response after the money has already moved, and two requests
may race for the same balance. Each has to produce one and only one transfer.

---

## Scope

In scope: `POST /transfers`, wallet balances, the double-entry ledger, the
transfer state machine, idempotent replay.

Explicitly **out of scope**, as optional enhancements in the brief:

| Not building | Why it matters here |
|---|---|
| Wallet balance API (`GET /wallets/{id}`) | No read path by wallet, so no index for one |
| Transfer history API | Same; see [Indexes](#indexes) |
| Authentication / multi-tenancy | Determines the idempotency key namespace; see below |
| Reversals, refunds, multi-currency, partial transfers | Each changes the ledger model |
| Metrics, tracing | Logging only; see [Observability](#observability) |

Wallets are assumed to already exist. Nothing in scope creates one, so wallets
are seeded directly (see [Testing](#testing)).

---

## API contract

### Request

```http
POST /transfers
Content-Type: application/json
```

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

`amount` is an integer in **minor units** (cents), never a decimal or float.
It decodes into an `int64`, so `100.5` is rejected at decode rather than
silently truncated. Unknown fields are rejected, so a misspelled field name
fails loudly instead of being ignored.

Validation, all producing `400`:

| Field | Rule |
|---|---|
| `idempotencyKey` | required, non-empty, at most 255 **bytes** (the limit caps storage, so it counts bytes rather than characters) |
| `fromWalletId` | required, non-empty |
| `toWalletId` | required, non-empty, different from `fromWalletId` |
| `amount` | required, integer, greater than zero |

### Response

One body shape for every outcome, success or failure, so a client parses one
thing:

```json
{
  "id": "9f8c1a4e-0f1e-4c2b-9a77-6b2d1f0c5a31",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "state": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-09-12T10:23:03Z"
}
```

`state` is the authoritative answer. A client never has to read the status code
to learn what happened to its money. That matters because the status code is
the part most likely to be mangled by a proxy.

Errors that never produced a transfer return:

```json
{ "error": { "code": "WALLET_NOT_FOUND", "message": "wallet wallet_9 does not exist" } }
```

### Status codes

| Code | When |
|---|---|
| `201 Created` | A new transfer was created and processed |
| `200 OK` | Replay of a key whose transfer already processed. Identical body |
| `400 Bad Request` | Malformed JSON, unknown field, or failed validation |
| `404 Not Found` | `fromWalletId` or `toWalletId` does not exist |
| `409 Conflict` | Key already used with **different** parameters |
| `422 Unprocessable Entity` | Insufficient funds, on the first call and on every replay |
| `503 Service Unavailable` | Lock timeout; the request is safe to retry unchanged |
| `500 Internal Server Error` | Anything unanticipated |

Replays also carry `Idempotent-Replay: true`.

The asymmetry is deliberate: a replayed success returns `200` rather than `201`
because nothing was created on that call, while a replayed failure returns `422`
both times because the failure *is* the answer and downgrading it to `200` would
invite a client to treat a failed transfer as a successful one. The body is
byte-identical across calls either way; only the status distinguishes "created
now" from "already existed". The alternative is to replay the original status
verbatim, so the caller cannot tell a replay from a first call. That is
defensible too, but it makes `201 Created` a lie on a call that created
nothing.

---

## Idempotency

`transfers.idempotency_key` carries a `UNIQUE` constraint. The key is claimed
with a single statement:

```sql
INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id,
                       to_wallet_id, amount, state)
VALUES ($1, $2, $3, $4, $5, $6, 'PENDING')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id;
```

A returned row means this request owns the key. **No row means someone else owns
it**, so the existing transfer is read and returned. The database arbitrates;
there is no check-then-insert window for two requests to slip through.

This is also why the claim is safe against a *concurrent* duplicate rather than
merely a sequential one: when another transaction holds an uncommitted row with
the same key, `ON CONFLICT DO NOTHING` blocks until that transaction commits or
rolls back. On commit, the second request sees a settled transfer; on rollback,
it claims the key itself. It can never observe `PENDING`.

**`request_hash`** is `SHA-256` over the *parsed* fields, hex-encoded. Hashing
parsed fields rather than the raw body means whitespace, key order, or an added
unknown field cannot cause a false `409`.

Each field is length-prefixed as well as newline-separated, so `wallet_1` becomes
`8:wallet_1\n`. The encoding therefore does not depend on which characters a
wallet id may contain. Separation alone would be ambiguous: wallet ids are opaque `TEXT`
and may contain newlines, and `"a\nbc" → "x"` and `"a" → "bc\nx"` would then
encode to the same bytes and fingerprint identically, so the second caller would
receive the first caller's transfer instead of a `409`.

The idempotency key itself is excluded from the hash. It is the lookup key, not
part of what is being fingerprinted. A replay whose hash differs
from the stored one returns `409`: the alternative, returning a transfer the
caller did not ask for, is worse than an error.

**No separate idempotency table.** ASSIGNMENT.md suggests an
`idempotency_records` table; the key is a `UNIQUE` column on `transfers` instead.
Every column such a table would hold is already on that row, and the response is
derivable from it, so a separate table adds a write per request and a second row
to keep consistent without adding a fact. It also makes the claim and the
transfer the same insert, so neither can exist without the other. The costs are
real: error responses are not idempotent, because a request that produced no
transfer claims no key, and keys cannot be expired separately from transfers,
which are never deleted. A second endpoint needing idempotency is the point to
build it.

**Namespace: global.** There is no authentication in scope, so a key is unique
across the whole service. The consequence is real and worth stating: two
independent callers who both choose `"abc123"` collide, and the second receives
either the first's transfer or a `409`. In a multi-tenant deployment the
constraint becomes `UNIQUE (client_id, idempotency_key)` and the key is scoped
to the authenticated caller. That is a one-column change, deliberately deferred
rather than half-built.

**Transfer ids** are UUIDv4 generated in Go, not by the database. The id is then
known before the statement runs, so it can be logged with the claim attempt
regardless of outcome, and the service does not depend on the `pgcrypto`
extension or a minimum PostgreSQL version for `gen_random_uuid()`. On a
conflicting claim the generated id is simply discarded.

---

## Transaction and concurrency

**The strategy is pessimistic row-level locking.** Both wallet rows are locked
before the balance is read, so the balance checked is the balance written
against. Optimistic locking was rejected: it degrades worst under exactly the
contention a wallet produces, and it needs a retry loop.

One transaction covers the whole transfer, at `READ COMMITTED`. That is
PostgreSQL's default, and it is sufficient *because* of those locks. Every balance read happens
under `FOR NO KEY UPDATE`, so the read-then-write race a stronger isolation
level would protect against cannot arise. `SERIALIZABLE` would add serialization
failures that must be retried in a loop, which is strictly more machinery for a
guarantee the locks already provide.

1. **Claim the idempotency key.** No row back → read the existing transfer and
   return it, taking no wallet locks. Replays are cheap.
2. **Lock both wallets**, as two separate statements issued in ascending wallet
   id order. Both are guaranteed to exist by now: step 1's foreign keys have
   already aborted the transaction if either does not.
   ```sql
   SELECT balance FROM wallets WHERE id = $1 FOR NO KEY UPDATE;
   ```
3. **Check sufficiency.** Short → mark `FAILED` with a reason, **commit**, return
   `422`.
4. **Move the money**, relative rather than absolute:
   ```sql
   UPDATE wallets SET balance = balance - $1, updated_at = now() WHERE id = $2;
   ```
5. **Insert both ledger entries**, one `DEBIT` and one `CREDIT`.
6. **Mark `PROCESSED`** with a guarded transition, and commit.

Correctness comes from holding the row lock across the read and the write. The
relative `UPDATE` is defensive style on top of that, not the thing that makes it
safe. Under the row lock an absolute write would be equally correct.

### Lock mode and ordering

`FOR NO KEY UPDATE`, not `FOR UPDATE`:

- Step 1's foreign keys take a `FOR KEY SHARE` lock on both wallet rows, which is
  how PostgreSQL stops a referenced row disappearing.
- `FOR UPDATE` **conflicts** with `FOR KEY SHARE`. Claiming the key and then
  reaching for `FOR UPDATE` asks to upgrade a lock every other in-flight transfer
  on that wallet already shares.
- Two transactions each waiting for the other to release a shared lock deadlock.
  Lock ordering cannot prevent that, because the cycle is an upgrade rather than
  an ordering.

`FOR NO KEY UPDATE` does not conflict with `FOR KEY SHARE` but does conflict with
itself, which is the mutual exclusion a debit needs. It is also the honest
strength: a transfer changes `balance` and `updated_at`, never the wallet's key.
This was found by the concurrent-debit test, which deadlocked with `40P01` on its
first run; the repository tests had missed it because they lock wallets without
first claiming a transfer, so there was no shared lock to upgrade from.

Two statements rather than one `WHERE id IN (...) ORDER BY id`, because a single
statement locks in sorted order only as a consequence of the plan sorting before
locking, which is a property of the planner rather than of the query. Two
statements make
the ordering a property of the code, and consistent ordering is what stops two
opposing transfers between the same pair from deadlocking.

### Why a failed transfer is committed

Rolling back would release the idempotency key, so a retry could re-attempt the
debit and succeed once the balance changed. The same key would produce two
different answers, which is what idempotency exists to forbid. Committing the `FAILED` row
binds the key permanently to that answer. A caller who tops the wallet up and
wants to retry must use a **new** key; that is a correct requirement, not a
limitation.

### The PENDING guard

`PENDING` never escapes the transaction, so the transfer is only ever observed
settled. It is not decorative: it names the state where the key is claimed but
the money has not moved, and it is the row that would survive if this became an
asynchronous or two-phase flow.

The transition out of it is guarded in SQL as well as in the domain, with
`WHERE id = $1 AND state = 'PENDING'` and a zero row count treated as an error.
**In the flow above that guard never fires**, because each transfer is
settled once inside the transaction that created it. It is defence in depth for
the day a `PENDING` row is committed and left for something else to finish, when
two actors could otherwise settle the same transfer. Retrying a whole request is
not that case: the claim conflicts on the key and never reaches the guard.

A zero row count also covers a transfer id that does not exist. Both are
programming errors, so they are not told apart.

### Wallet existence

The claim in step 1 carries foreign keys to `wallets`, so an unknown wallet
raises `23503` and aborts the transaction. The handler maps that to `404`,
reading `transfers_from_wallet_fk` or `transfers_to_wallet_fk` from the error to
name the offending wallet. That is why those constraints are named explicitly in
the migration rather than left to PostgreSQL's defaults.

Because the transaction aborts, **the key is not claimed**. A later valid request
reusing that key succeeds rather than returning `409`. That is correct: the first
attempt created nothing, so there is no prior result to be idempotent about.
Validation failures behave the same way. They are rejected before any database
work.

**Only one wallet is named when both are missing.** PostgreSQL validates the two
foreign keys in declaration order and stops at the first violation, so such a
request is blamed on `fromWalletId` alone. Swapping the declaration order swaps
the blame, which is how this was verified. The caller fixes that one, retries, and only then
learns the destination is also unknown.

Reporting both would need a pre-flight existence query before the claim. Not done
here: naming two unknown wallets is a caller bug, and a second round trip is a
fair price for keeping the existence check in one place.

**A replay never reaches the foreign key.** `ON CONFLICT DO NOTHING` inserts no
row when the key is already held, and foreign keys are validated only on an
actual insert. This was verified too. So replaying a known key with a nonexistent wallet
id produces no `404`. It takes the ordinary replay path, and because wallet ids
are part of `request_hash`, the mismatch returns **`409`** instead.

That is the better answer: the key is already bound to a real transfer, so "this
key was used with different parameters" describes the problem more precisely than
"that wallet does not exist", and the stored transfer is what the caller is
actually in conflict with. The consequence to be aware of is that `404` and `409`
are not interchangeable probes for the same condition. Which one an unknown
wallet produces depends on whether the idempotency key was free.

---

## Failure modes

| Failure | Behaviour |
|---|---|
| Duplicate request, same parameters | Original transfer returned; deduplicated by the unique constraint, so no second transfer and no second ledger pair |
| Duplicate request, different parameters | `409`; the stored transfer is untouched |
| Response lost after commit | Retry replays the committed transfer. This is why the key is claimed in the same transaction that moves the money |
| Process crashes mid-transfer | Transaction rolls back; key unclaimed; no partial ledger; safe to retry |
| Insufficient funds | `FAILED` committed, `422`, no money moved, same answer on every replay |
| Unknown wallet, key free | `404`; nothing written, key left unclaimed. If both wallets are unknown only `fromWalletId` is named |
| Unknown wallet, key already held | `409`. The claim inserts no row, so the foreign key is never validated and the hash mismatch answers first |
| Concurrent debits of one wallet | Serialised by the row lock; the second sees the first's balance |
| Contention exceeds `lock_timeout` | `55P03` → `503`; safe to retry unchanged |
| Lock upgrade from the foreign key's `FOR KEY SHARE` | Prevented by locking with `FOR NO KEY UPDATE`; `FOR UPDATE` deadlocks under concurrent transfers on one wallet |
| Overdraft attempted despite the check | `CHECK (balance >= 0)` aborts the transaction → `500`. Unreachable by design; the constraint exists so a logic bug corrupts nothing |
| Balance overflows `BIGINT` | `22003` → `500`. Not defended against further; the bound is ~9.2×10¹⁸ minor units |
| A request outlives its usefulness | Bounded for the cases this design creates, but not by an explicit deadline. See below |

A constraint violation aborts the whole transaction in PostgreSQL, which is why
the sufficiency check under the lock is the real path and `CHECK (balance >= 0)`
is only the backstop: the service cannot catch it and write `FAILED` in the same
transaction without a savepoint.

**There is no per-request deadline.** `ReadTimeout`, `WriteTimeout` and
`IdleTimeout` bound the socket, not the handler: `WriteTimeout` arms a deadline
on the connection and bites only when the handler finally writes, so a two-second
`WriteTimeout` lets a six-second handler run all six with its context never
cancelled. It never bounds how long a transfer holds its wallet locks.

Four things do bound a request, between them covering the cases this design
produces: `lock_timeout` for lock waits, primary-key lookups everywhere so no
query runs long, a pool that drains because every holder is itself bounded, and
context cancellation when a client disconnects. That leaves a PostgreSQL
connection hanging mid-query as the one unbounded case. A middleware setting a
deadline on the request context, mapped to `503` so the status contract does not
change, is the first thing to add, and is left out because that exposure is
narrow.

---

## Retry behaviour

Every operation here is retry-safe with the **same** key. That is what the key is
for. `503` and `500` should be retried. `400`, `404`, and `409` are terminal; retrying changes nothing. `422` is
terminal for that key: the transfer failed and will keep reporting that it
failed. Retrying the *intent* requires a new key.

---

## Indexes

Index usage: everything is reached by an index that already exists for another
reason.

- `wallets` and `transfers`, by primary key.
- `transfers`, also by `idempotency_key`. Its `UNIQUE` constraint is a B-tree
  index.
- `ledger_entries`, by `transfer_id`. `UNIQUE (transfer_id, type)` is likewise a
  B-tree, and a B-tree on two columns serves a lookup on the first alone.
  Confirmed with `EXPLAIN` over 10,000 entries: one transfer's pair is found by
  index scan, not by scanning the table.

That last point is a property of any unique constraint leading with
`transfer_id`, not a benefit of the narrower key. The narrowing is justified by
correctness alone: including `wallet_id` would permit two `DEBIT` rows on one
transfer.

The single explicit secondary index is `(wallet_id, id)` on `ledger_entries`, for
per-wallet reads and for the test asserting stored balance equals the sum of a
wallet's entries.

**No index exists for querying transfers by wallet**, because nothing queries
them that way. Transfer history is out of scope, so adding one now would be
speculative. A transfer-history endpoint would want
`(from_wallet_id, created_at DESC)`, and a symmetric index on `to_wallet_id`
only if incoming history were exposed too; `to_wallet_id` is deliberately left
unindexed until a reader exists.

---

## Consistency expectations

- The ledger always balances: every transfer has exactly one `DEBIT` and one
  `CREDIT` of equal amount, on its own two wallets. This one is enforced by
  construction rather than by the schema. See [below](#the-ledger-invariant).
- A wallet's stored balance always equals the sum of its ledger entries. Stored
  balance is the operational value; the ledger is the audit record. A test
  asserts they agree.
- No ledger entry can exist without its transfer (foreign key), and no transfer
  can reference a wallet that does not exist (foreign key).
- A transfer is `FAILED` if and only if it carries a `failure_reason`.

### The ledger invariant

Worth being exact about, because it is the one guarantee here the database does
not fully hold. `UNIQUE (transfer_id, type)` stops a transfer carrying two
`DEBIT` rows or two `CREDIT` rows, which is why the key excludes `wallet_id`. It
says nothing about absence. These are all storable:

- a `PROCESSED` transfer with no ledger entries at all
- a transfer with only a `DEBIT`
- a `DEBIT` of 100 against a `CREDIT` of 7
- both entries on a wallet the transfer never mentions

What actually holds the invariant is `domain.LedgerEntriesFor`, which builds both
entries from one transfer, both amounts from the same field and both wallets from
the transfer's own, so there is no code path that can produce an unbalanced
pair. They are written in the transfer's transaction, and
`TestStoredBalanceAlwaysEqualsTheLedger` checks stored balances against the
ledger across a run of transfers including a failed one.

Closing the gap in the database would take a constraint trigger, deferred to
commit time because the ledger is legitimately half-written between the two
inserts. That is left out deliberately:

- Every other guarantee here is a declarative constraint, `CHECK`, `UNIQUE` or
  `FOREIGN KEY`, visible in the schema and free to reason about.
- A trigger is procedural code that happens to live in the database: invisible
  from the Go side, running on every transfer, needing tests of its own.

At this scope the line is drawn at what a constraint can express, and this
invariant is past it.

---

## Observability

Structured logging via stdlib `log/slog`, one record per request carrying
transfer id, idempotency key, both wallet ids, amount, outcome state, whether it
was a replay, and duration. That is enough to answer "what happened to this
transfer" and "is this caller retrying" from logs alone. Amounts are logged;
wallet ids are opaque identifiers, not personal data.

Metrics and tracing are out of scope. The counters worth adding first would be
transfers by outcome, replay rate, and lock-timeout rate.

---

## Testing

Integration tests run against a **real PostgreSQL**. The database is never
mocked: a mocked query passes happily while the SQL underneath it is wrong, and
the SQL is where every guarantee in this document actually lives.

A missing database is a convenience locally and a failure in CI. With
`TEST_DATABASE_URL` unset, `go test ./...` skips the integration tests and
passes, so a fresh clone gives a clear message rather than a dozen connection
errors. With `CI` set, which GitHub Actions does automatically, the same
condition fails instead, because a skipped test and a passing test are both
green and a pipeline that silently stopped exercising the SQL would report
success while testing nothing. The workflow fails too if Postgres cannot start.

The rule those two enforce together: **absence of a test result is a failure in
CI and a convenience locally.** The check lives in the test fixture as well as
the workflow so it holds however CI is wired. A later edit to `ci.yml` cannot
quietly disable these tests without the suite objecting.

Wallets are seeded by a test helper inserting rows directly, since no endpoint
creates them. Each test works on its own wallet ids so tests do not interfere.

Behaviour covered:

- happy path: balances move, exactly two ledger entries, transfer `PROCESSED`
- duplicate key: original transfer returned, no second transfer, no second pair
- duplicate key, different parameters: `409`, stored transfer untouched
- insufficient funds: `FAILED` committed, no money moved, replay returns `422`
- unknown wallet: `404`, nothing written, key left unclaimed
- known key naming an unknown wallet: `409` rather than `404`, since the
  claim never reaches the foreign key
- concurrent debits of one wallet: no double spending, no negative balance
- invariant: every wallet's stored balance equals the sum of its ledger entries

Assertions are behavioral: balances, entries, states and status codes, not
assertions about which queries ran.

### Schema verification

`TestSchemaRejectsInvalidWrites` attempts, one at a time, every write the schema
is supposed to make impossible. 17 assertions, each checking the SQLSTATE *and*
the constraint name produced.

Most of those constraints are backstops the application never reaches: the
domain rejects a negative amount or a self-transfer long before SQL sees it. That
is precisely why they need a test of their own, since nothing else in the suite
would notice one being weakened. Three schema mutations confirm it does: renaming
`transfers_from_wallet_fk`, widening `UNIQUE (transfer_id, type)` to include
`wallet_id`, and dropping the `failure_reason` check each fail it.

Asserting the constraint *names* is deliberate. They are contract. The handler
reads `transfers_from_wallet_fk` off a `23503` to decide which wallet to name in
a `404`, so renaming one in a migration would quietly turn that `404` into a
`500`. Nothing else holds that coupling.

The migration is idempotent across repeated runs, and `lock_timeout` was measured
firing at 3.02s under real row contention.
