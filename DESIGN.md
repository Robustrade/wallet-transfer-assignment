# Wallet Transfer Service Design

## Problem and scope

Implement `POST /transfers` with atomic wallet-to-wallet transfers, correct
balances under concurrency, a balanced ledger, and retry-safe results when an
idempotency key is supplied. Keep the design small and explainable; add no queues,
workers, caches, or other infrastructure without a requirement.

Use Go 1.21, standard `net/http`, and `database/sql` with
`github.com/jackc/pgx/v5/stdlib`. PostgreSQL is chosen because it is the preferred
persistence option in `ASSIGNMENT.md`. Select dependency versions compatible with
Go 1.21. Flow: HTTP handler → service → repository → PostgreSQL. Pass
`context.Context` through every layer and into database calls. Handlers handle
transport and input validation; services own business rules and orchestration;
repositories own SQL and transaction access.

## API contract and expected behavior

The assignment specifies this request shape:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Money and stored balances use `int64` minor units, never floating point. Decode
amount directly into an integer; reject fractions and values outside `int64`.
Proposed validation: nonempty wallet IDs, distinct source and destination, and
positive amount. Check sufficient source funds and destination overflow while
holding wallet locks. Assume one shared currency/unit; no conversion or fees.

Proposed response contract (status codes and fields are not assigned by the spec):

| Outcome | HTTP | Body |
| --- | --- | --- |
| Committed success | 201 | `transferId`, `state: "PROCESSED"`, `fromWalletId`, `toWalletId`, `amount` |
| Invalid request before transfer creation | 400 | `error.code`, `error.message`; no transfer ID |
| Committed business rejection | 422 | `transferId`, `state: "FAILED"`, `error.code`, `error.message` |
| Existing key with different transfer parameters | 409 | `error.code`, `error.message` |
| Database unavailable or transient transaction failure | 500 | `error.code`, `error.message`; no claim of persisted failure |
| Unexpected internal failure | 500 | Generic error without internal details |

A completed duplicate returns the stored original HTTP status and body, including
the original transfer ID and any business rejection. Send a terminal transfer
response only after commit succeeds. HTTP delivery itself is not guaranteed;
idempotency makes repeated attempts safe.

## Persistence, balance, and ledger invariants

- `wallets` stores the current balance. Update balances in the same transaction as
  the ledger; do not derive the API's current balance by summing ledger entries.
- `transfers` stores identity, source/destination, amount, state, nullable
  idempotency key, and original transfer parameters. `idempotency_records` stores
  the unique key, deterministic request fingerprint, matching transfer reference,
  and terminal response status/body for replay. See `migrations/README.md` for
  canonical fingerprint encoding and deferred foreign-key requirements.
- A database unique constraint on the non-null idempotency key is the final
  concurrency guarantee. Never rely on an in-memory check or a read-before-write.
- Each successful transfer has exactly one positive DEBIT for the source and one
  equal positive CREDIT for the destination. Their signed sum is zero, and total
  wallet funds are conserved. Failed transfers have no ledger entries or balance
  changes.
- Use ledger foreign keys, valid-type/positive-amount checks, and uniqueness on
  `(transfer_id, entry_type)`. Uniqueness prevents extra entries; it does not alone prove
  that both exist. The atomic workflow inserts both before marking PROCESSED,
  and integration tests verify the complete invariant.
- Enforce nonnegative wallet balances and allowed states with database checks.
  Prevent integer overflow before arithmetic. Financial writes go through this
  workflow; arbitrary direct database mutations are outside this service contract.

## Transaction, concurrency, and states

Use one PostgreSQL transaction for the complete financial transfer, with
READ COMMITTED isolation and explicit row locks:

1. Claim the key in `idempotency_records` with a pre-generated transfer ID, using conflict-safe SQL such as
   `INSERT ... ON CONFLICT DO NOTHING`. Concurrent duplicates wait for the
   competing transaction; a subsequent statement reads its committed result.
   On conflict, compare fingerprints and replay the result or return 409. Deferred
   foreign keys require a matching transfer before commit.
2. Lock the two wallet rows using `SELECT ... FOR UPDATE`, acquiring them in
   deterministic wallet-ID order regardless of transfer direction. Every transfer
   uses the same ordering. Insert the PENDING transfer after acquiring these locks
   so wallet FK checks do not first acquire shared locks that later need upgrading.
   Evaluate funds and overflow using the locked balances.
3. On success, update both stored balances, insert exactly two ledger entries,
   and transition PENDING → PROCESSED. Persist the replay result and commit.
4. If an expected business rule fails after creation, before financial writes,
   transition PENDING → FAILED, store its replay result, and commit where the
   schema/workflow permits. Insufficient funds is such a failure.

Only PENDING → PROCESSED and PENDING → FAILED are valid transitions. Guard updates
by the previous state; terminal states cannot be executed again. PENDING lives
inside the transaction; this synchronous design does not commit unfinished work
or need a background recovery worker.

Any SQL/infrastructure failure rolls back the transaction; do not try to persist
FAILED in an aborted transaction or claim that rollback saved a failure row.
Deterministic ordering reduces deadlock risk but does not eliminate all possible
database errors. Context cancellation also stops database work and triggers cleanup.

## Idempotency and retries

Proposed default: keys are optional, globally scoped to this endpoint, and retained
without expiry for this assignment. Without a key, each request is independent and
a lost response cannot safely be retried with exactly-once effects. A retry must
reuse the same key and transfer parameters; a new key represents a new attempt.

If commit succeeded but the HTTP response was lost, replay the persisted result
without changing balances or creating ledger entries. Persisted FAILED results
also replay, even if funds subsequently become available. Pre-creation validation
errors do not reserve keys.

If the transaction rolls back, its key claim rolls back too, so a later attempt
can execute. A connection error during commit can leave the outcome unknown:
return a retriable infrastructure error, never assert FAILED, and resolve by
retrying with the same key. No automatic server retries initially; clients may
use bounded backoff for transient failures. Do not blindly retry business errors.

## Side effects, failures, and observability

Success commits one transfer, two balance updates, two ledger entries, and replay
data. A committed business failure stores only the transfer/failure replay data.
No external financial calls, emails, or events are required.

Cover malformed input, missing wallets, self-transfer, invalid amount,
insufficient funds, destination overflow, conflicting key reuse, concurrent
duplicates/debits, lock timeouts/deadlocks, SQL errors, cancellation, and lost
responses. Roll back partial financial writes on any failure.

Use structured logs with request/transfer ID, outcome, replay indicator, duration,
and error category. Distinguish business rejection, rollback, and unknown commit
outcome. Avoid raw request bodies and database configuration in logs. Log the idempotency key as explicitly requested for tracing. Record success
only after commit; logs are not the financial source of truth. Metrics and tracing
infrastructure are optional, not prerequisites.

## Testing strategy

For every new business behavior: **Red** — add a failing behavior test;
**Blue** — implement the smallest correct change; **Green** — refactor with tests
still passing. Test observable results and persisted invariants rather than
private methods or exact SQL strings.

Use unit/HTTP tests for validation, domain transitions, and transport mapping.
Use real PostgreSQL integration tests for successful balance/ledger changes,
business failures, rollback after partial writes, duplicate replay (including
failed results and simulated lost HTTP responses), parameter conflicts, and
concurrent requests. Exercise competing debits, same-key races, opposite-direction
transfers, and actual lock blocking with coordinated connections. Assert no
negative balances, conserved funds, terminal states, and exactly two correct
ledger entries per successful transfer. Mocks or SQLite cannot substitute for
these PostgreSQL transaction/locking tests.

## Requirements still ambiguous

- The assignment does not specify HTTP status codes or response fields; the
  contract above is proposed, not an existing requirement.
- Key optionality, scope, retention, are not fully
  specified. The proposed optional/global/no-expiry choices need confirmation;
  mismatched parameters return a conflict as explicitly requested by the user.
- Currency, initial wallet funding, wallet creation, and authentication are not
  defined. Assume pre-existing seeded wallets sharing one currency/unit.
- The assignment says “every transfer” has two ledger entries. Interpret this as
  every successful transfer, consistent with the chosen invariant; FAILED must
  not move money or create financial ledger entries.
- Missing-wallet persistence depends on schema design: wallet foreign keys can
  prevent creating a transfer referencing a nonexistent wallet. Proposed default:
  reject before creation with 404 and no transfer ID/key reservation when those
  foreign keys prevent storing FAILED. Confirm whether such failures must replay.
- No durable/asynchronous PENDING workflow or server retry policy is required.
  The chosen synchronous transaction keeps PENDING uncommitted.

## Repository state

Before this document, the working tree was clean on `solution/nikhil-nile`, at the
same commit as `main` (`8b97363`). Keep that existing feature branch. This step
adds documentation only; application implementation and tests are future work.

## Service foundation

Require `DATABASE_URL`; default `HTTP_ADDR` to `:8080`. Pool defaults are 10 open
and 5 idle connections, with a 30-minute connection lifetime. Startup database
ping has a 5-second deadline; graceful shutdown has 10 seconds. These settings
are environment-configurable and invalid values fail startup. `GET /health`
returns HTTP 200 and `{"status":"ok"}` as a process liveness check, not a database
readiness probe. Other methods return 405. Startup requires a successful database
ping. SIGINT/SIGTERM stops accepting requests, drains active handlers, then closes
the pool; on drain timeout, cancel request contexts and force HTTP connections
closed. No schema or transfer behavior is introduced in this foundation.

## Domain layer

Domain types use string IDs, `int64` minor-unit balances/amounts, and `time.Time`
timestamps, with no HTTP or database dependencies. Transfer creation validates
positive amount and distinct wallets and starts PENDING. Status is private and
read through `Status()`; only `MarkProcessed(at)` and `MarkFailed(at)` can change
it, and only from PENDING. Callers supply timestamps; rejected transitions leave
the object unchanged. The zero-value transfer has an invalid state. Typed status
and ledger-type values support validation; ledger validation requires a positive
amount and DEBIT or CREDIT. Persistence reconstruction is deferred to the
persistence design rather than adding an unrestricted status setter now.

## Repository contracts

`internal/repository` defines only two interfaces: `UnitOfWork` for the service's
atomic callback and `Tx` for all operations within it. The PostgreSQL adapter will
bind that callback to exactly one READ COMMITTED `*sql.Tx`; no repository method
may start another transaction or write through the pool. The service decides the
callback boundary and operation order; the adapter handles begin/commit/rollback,
returns commit errors, rolls back on callback errors/panics, and does not retry.
Read-only operations can use a short unit of work rather than adding a second
repository interface now. SQL handles never reach the domain or service.

Wallet locking returns source/destination in argument order while acquiring locks
in deterministic ID order. Transfer status writes are explicit guarded terminal
transitions. Idempotency reservation reports an existing key without aborting the
transaction; the service reads and compares its fingerprint afterward. Completion
matches the pre-reserved transfer ID and cannot overwrite a stored result.
`ErrNotFound` and `ErrConflict` describe expected persistence outcomes; unexpected
infrastructure errors remain errors and cause rollback, not business FAILED
results. An expected business failure that should persist must be recorded inside
a callback that returns nil, with its business outcome returned separately by the
service after commit. The repository result type carries status/body replay data
without importing HTTP. SQL implementations and PostgreSQL integration tests are
not part of this contracts-only step.

## PostgreSQL adapter

The repository adapter now binds all operations to one `sql.Tx` using
context-aware queries. Ordered `FOR UPDATE` rows are mapped back by wallet ID,
not by row position. Key reservation uses targeted `ON CONFLICT DO NOTHING`;
ordinary unique violations are both conflicts and transaction failures, never
permission to continue SQL. Database failures poison the scoped adapter, blocking
further SQL and commit even if a callback swallows the error. Deadlock (40P01) and
serialization (40001) failures additionally carry a retryable-transaction marker.
Retries require a new whole unit of work. Wrapped errors preserve their causes
for `errors.Is/As` but their displayed messages omit driver details; future HTTP
handlers must still map errors explicitly to public responses.

Persisted transfers are reconstructed via the existing constructor and explicit
terminal transitions, without introducing a generic domain status setter.
Unit tests cover error classification, failure short-circuiting, and domain
reconstruction. Real PostgreSQL verification of migrations, locks, concurrency,
commit/deferred-constraint failures, and rollback remains integration-test work.

## Core transfer service

`Service.Transfer` validates before opening a unit of work. The callback reserves
or replays a key, locks both wallets, creates PENDING, then checks sufficient funds
and destination `int64` capacity before mutation. Insufficient funds or overflow
commits FAILED plus its replay result without balance/ledger writes. Missing
wallets roll back the reservation without creating a transfer. Every persistence
error exits the callback; the existing deferred rollback covers all early returns.
The service returns no result if commit fails, including ambiguous commit outcomes.
No transaction retry loop is introduced. Structured completion logs are emitted
after the unit of work returns, with duration, outcome, replay flag and transfer
ID; failures are categorized without logging driver errors or database configuration.

## Idempotency arbitration and visibility

The claim now uses `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING RETURNING
idempotency_key`. A returned row owns the claim; no row means a duplicate, without
aborting PostgreSQL's transaction. The loser reads the record in a separate
statement, obtaining a fresh READ COMMITTED snapshot after the unique-index wait.
Do not combine this read with the insert in a single statement/snapshot.

No externally visible IN_PROGRESS state is needed: reservation, transfer, money,
ledger, and final response commit together. A contender waits on an uncommitted
owner. If that owner commits, the contender compares the fingerprint before
replaying its complete original result (including FAILED). If the owner rolls
back, the contender can insert its own claim and execute. A missing result in a
committed record violates the workflow; return ErrIncompleteResult and do not
reexecute. This is distinguishable from normal in-progress work, which stays
uncommitted and causes the insert to wait. No schema/state column change is made.

Fingerprinting uses only the ordered source ID, destination ID, and exact integer
amount. Invalid UTF-8 IDs/keys are rejected before reservation because JSON's
replacement of invalid bytes could otherwise collapse different business IDs
into the same fingerprint. Keys themselves are excluded from the fingerprint.
No process-local cache or pre-insert SELECT decides ownership.

Tests cover discarded committed responses using a fresh service instance,
incomplete records, payload conflicts, and FAILED replay. PostgreSQL tests pause
an owner after its claim, observe the contender in pg_blocking_pids, then exercise
commit, rollback/takeover, conflicting payloads, and committed business rejection.
They assert one transfer/record, one balance movement, and exactly two ledger
rows on success (zero on business failure). These tests now run under the integration build tag using Testcontainers;
a missing Docker runtime fails the suite rather than skipping verification.

## Double-entry ledger review

The service writes both ledger legs on the same transaction as balances, then
marks PROCESSED. Either insert error returns immediately and rolls back the whole
unit of work. The existing `ledger_transfer_type_unique` constraint remains
`UNIQUE (transfer_id, entry_type)`; no schema or service change was needed.

The shared `assertBalancedTransferLedger` test helper checks exactly two entries,
one positive DEBIT and one positive CREDIT, the same transfer reference, each
amount equal to the transfer amount, correct source/destination wallets, distinct
entry IDs, and equal debit/credit amounts without potentially overflowing a sum.
Tests apply it to both transfer directions and persisted results before/after
replay, including concurrent same-key tests. PostgreSQL tests additionally attempt
both duplicate entry types using fresh primary keys and inject actual FK errors
on either ledger insert to verify unchanged balances and no surviving rows.


## Transfer HTTP endpoint

`POST /transfers` is wired through the service to the PostgreSQL unit of work.
The net/http handler uses encoding/json, a 1 MiB body limit, direct int64 decoding,
and rejects unknown fields and trailing JSON values. Missing/blank wallet IDs,
nonpositive amounts, and identical wallets return 400. Keys remain optional.
Transport and mapped errors use `{"error":{"code":"...","message":"..."}}`.
Service errors map to 400 (invalid), 404 (missing wallet), 409 (key conflict), or
500 (unexpected/database/incomplete result); internal error text is never emitted.
Committed results retain the exact service status/body: 201 for PROCESSED and 422
for FAILED, including the original transfer ID and business error on replay.
The handler performs no transaction, lock, balance, ledger, or idempotency work.
Handler tests use a service stub to verify transport mapping and exact replay
preservation; service/PostgreSQL tests separately verify financial side effects.


## Testcontainers integration lifecycle

Integration tests use the PostgreSQL Testcontainers module pinned to v0.30.0 for
Go 1.21. One lazy package-level container is shared; each test/subtest has a unique
schema, applies migrations, opens its own pool, and seeds only its own data.
No truncation or test ordering is needed for isolation. Startup waits for both
PostgreSQL readiness logs and its listening port; the migration connection also
verifies SQL availability. Cleanup closes pools, drops schemas, then terminates
the shared container with an independent bounded context. The integration build
tag makes Docker a deliberate test dependency without hiding failures as skips.
No mocks implement FOR UPDATE, FK/unique enforcement, rollback, or concurrent
arbitration in this suite. Existing service unit-test doubles remain only for
fast business-orchestration checks.

## Opposite-direction lock-order review

The current single-table `WHERE id IN ($1,$2) ORDER BY id FOR UPDATE` query sorts
before acquiring row locks. Both A→B and B→A lock A then B, irrespective of
parameter order; an ordered index scan can supply the same order without a Sort
node. This depends on wallet IDs remaining immutable and every participant using
the same database ordering. The service updates balance/timestamps only. Do not
move FOR UPDATE into an unordered subquery and sort its results afterward.
PostgreSQL documents that concurrent updates to ordering columns can change the
returned order while a READ COMMITTED query waits; wallet-ID mutation by external
writers is outside this guarantee. Primary keys alone do not prohibit ID updates.
Source/destination are mapped by ID after scanning, not by sorted row position.

The service claims one key first, obtains both wallet locks before inserting the
transfer (and thus before wallet FK key-share checks), then performs the financial
writes while holding the locks. It never locks the source separately first.
The opposite-direction test holds A externally, waits for both service queries to
block, and verifies B is still lockable with NOWAIT. After releasing A, both must
commit; unequal amounts verify business roles through balances and ledger entries.

This removes the simple two-wallet circular wait, not all possible deadlocks.
Other writers taking wallets in a different order, FK lock upgrades before wallet
locking, additional table/advisory/DDL locks, multiple transfer/key claims in one
transaction, or concurrent wallet-ID changes can introduce cycles. Future flows
must respect the entire resource order, not merely sort each pair independently.
SQLSTATE 40P01 and 40001 continue to abort the whole unit of work and surface as
transaction failures. There is no in-transaction recovery, retry loop, or Go mutex.

References: [PostgreSQL SELECT locking clauses](https://www.postgresql.org/docs/16/sql-select.html#SQL-FOR-UPDATE-SHARE)
and [deadlocks](https://www.postgresql.org/docs/16/explicit-locking.html#LOCKING-DEADLOCKS).


## Structured transfer events

The service requires an injected `*slog.Logger`; a nil logger is a constructor
programming error rather than a fallback to a global logger. The server supplies
its JSON logger. Events carry transfer_id, idempotency_key, from_wallet_id,
to_wallet_id, and status. request_accepted records the candidate transfer ID and
PENDING before transaction execution; duplicate/conflict events identify the
original transfer after lookup. This initial PENDING is processing intent, not a
claim that a row has committed.

Events include request_accepted, duplicate_idempotent_request,
insufficient_balance, transfer_committed, transaction_failed, and
idempotency_conflict. Commit/replay events are emitted only after the unit of work
succeeds. Insufficient balance records a business-rule observation while PENDING;
only the later commit event claims a persisted FAILED state. Transaction errors
use status UNKNOWN because commit failure may be ambiguous; no raw error text,
SQL, database URL, credentials, or per-query logging is emitted. Completion events
include duration and retryable_transaction. Structured logging tests cover these
outcomes, including failed-result replay and commit failure.


## Connection and shutdown review

The shared sql.DB pool uses 10 maximum open connections, 5 maximum idle
connections, 30m maximum lifetime, and 5m maximum idle time. All are env-configured;
startup performs PingContext with a 5s deadline and closes the pool on failure.
HTTP uses 5s header, 10s read, 15s write, and 60s idle timeouts. A configurable
10s request-context deadline bounds service and DB work independently of socket
write deadlines. Request contexts flow through the handler, service, BeginTx,
and every query/exec; client disconnects and deadline expiry cancel that work.

SIGINT/SIGTERM initiates HTTP Shutdown using a fresh 10s context. The base request
context deliberately survives the signal so active handlers can drain. Drain
timeout cancels that base context and force-closes HTTP connections. Deferred
cleanup cancels remaining request work and closes sql.DB after the HTTP lifecycle
ends. Commit failures remain ambiguous; cancellation must never imply a committed
transfer was undone. No additional configuration framework is introduced.

## Correctness review follow-up

HTTP rejects malformed UTF-8 and unpaired Unicode surrogate escapes before JSON
can replace them, preserving identifier/key identity. The service rejects NUL in
wallet IDs and idempotency keys before opening a transaction, since PostgreSQL
TEXT cannot represent it. Panic unwinding logs transaction_failed with UNKNOWN
status and re-panics without logging sensitive panic details; it cannot report a
commit merely because the named error return remains nil.

Review confirmed that all service writes use the same transaction, wallet rows
are consumed and closed before further statements, row-iteration errors propagate,
and duplicate key arbitration avoids continuing after a constraint violation.
Exactly-two/matching ledger entries remain a service invariant; the DB uniqueness
constraint alone only enforces at-most-one entry of each type. Tests and the race
detector pass for the default suite; integration-tag compilation is not evidence
of live PostgreSQL correctness. Docker execution remains required before claiming
verified locking, rollback, or concurrent financial behavior.
