---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0003 — Ordered pessimistic row locks with bounded retry for wallet debits

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

Two transfers can attempt to debit the same wallet simultaneously, or two transfers can move
funds between the same pair of wallets in opposite directions at the same time. The system must
guarantee correct balances, no double-spend, and consistent ledger entries under both cases —
and per an explicit steer during design review, the strategy should hold up as a real production
default, not just pass a two-thread test in isolation: it must not let a hot wallet under load
queue requests indefinitely, and it must recover from transient contention rather than fail on
the first collision.

## Decision drivers

- No read-then-write race on wallet balance under concurrent same-wallet debits.
- No deadlock between two transfers on the same wallet pair moving funds in opposite directions.
- Bounded behavior under contention: a blocked request must fail fast and recoverably, not queue
  forever and exhaust the connection pool.
- Simple enough to implement and test correctly within the assignment's time-box, without
  pretending to solve horizontal scaling for a single hot wallet — that's a different problem.

## Considered options

1. **Optimistic locking** (a `version` column on `wallets`; `UPDATE ... WHERE version = X`, retry
   the whole operation on a zero-row update). Rejected as the default: re-runs the entire business
   logic attempt on every conflict rather than just waiting for a lock, and risks livelock under
   sustained contention on a genuinely hot wallet — no better predictability than pessimistic
   locking with bounded retry for the contended case that actually matters here, at higher
   implementation complexity (two rows — debit and credit — both need to agree).
2. **`SERIALIZABLE` isolation with application-level retry on serialization failure.** Rejected as
   the default: the right tool when the conflicting rows aren't known in advance (write skew
   across predicates) — that's not this problem, we know exactly which two wallet rows are
   involved. Using SSI here adds retry complexity and more false-positive aborts under contention
   without buying additional safety over locking the known rows directly.
3. **Pessimistic row-level locks (`SELECT ... FOR UPDATE`) on both wallets, acquired in a
   consistent order, with an explicit lock timeout and bounded retry on the caller side.**
   *Chosen.*

## Decision

Within a single database transaction per transfer attempt:

- Acquire `SELECT ... FOR UPDATE` locks on both the source and destination wallet rows, always in
  **ascending `wallet_id` order**, regardless of transfer direction. This is what actually
  prevents the deadlock case: two opposite-direction transfers on the same pair would otherwise
  each hold one wallet's lock while waiting on the other's.
- **This explicit lock must be acquired before the transfer row is inserted, not after (found via
  real-Postgres testing during implementation — corrected alongside ADR-0002's matching note).**
  `INSERT INTO transfers` takes an implicit `FOR KEY SHARE` lock on each wallet row it references,
  as part of the FK check — acquired in whatever order the columns happen to be checked, not in
  the ascending-`wallet_id` order above. Two concurrent attempts sharing a wallet can each hold
  that implicit lock from their own transfer INSERT while blocked on the *explicit* `FOR UPDATE`
  the other already holds via its own implicit lock — a real, repeatable `deadlock_detected`, not
  a theoretical one. Locking first means the explicit `FOR UPDATE` is already the strongest lock
  held on both rows by the time the transfer INSERT's own FK check runs against them, so that
  check never has anything to contend with. The in-memory fake used for ST1-ST7 (ADR-0006) cannot
  reproduce Postgres's implicit FK locking, which is exactly why this was only caught once CT1/CT7
  ran against a real database, not before.
- Set an explicit `lock_timeout` (e.g. 2s) for the transaction (`SET LOCAL lock_timeout`, once per
  transaction — scoping clarified on review). It governs every lock wait inside that transaction,
  not just the wallet `SELECT ... FOR UPDATE` calls: the same timeout also bounds how long a
  concurrent duplicate can block on ADR-0002's idempotency-key unique-insert conflict, since that
  wait is a lock wait too. A request blocked behind another fails fast with a distinct, retryable
  error instead of queuing indefinitely.
- The service layer wraps the transaction attempt in a **bounded retry** (e.g. up to 3 attempts,
  exponential backoff with jitter, via `asyncio.sleep()` — never a blocking sleep, per ADR-0007)
  for three error classes: a `lock_timeout`, Postgres's own `deadlock_detected` (originally framed
  as defense-in-depth on the theory that the ordering rule above made it unreachable — revised
  after real-Postgres testing showed it *was* reachable, via the implicit-FK-lock interaction
  documented above, until the wallet-locking-order fix; kept in the retryable set both as the fix
  for that and as defense-in-depth against any future code path that doesn't follow the ordering
  rule), and a
  connection-pool-acquire timeout (added on review — see the pool-sizing note below for why this
  is a distinct failure mode, not covered by the other two). **Insufficient funds is not
  retried** — it's a legitimate terminal result (`FAILED`), not a transient error, and is returned
  as `200` (ADR-0008): a correctly handled business outcome, not an HTTP-level failure.
- **Connection-pool sizing (gap found on review):** ADR-0007's `asyncpg.create_pool` has a finite
  size; CT6 (below) drives 50-100 concurrent attempts against one wallet, which previously had no
  stated relationship to pool capacity. If the pool is smaller than the concurrency a request
  actually needs to sustain, a request can fail trying to *acquire a connection* before it ever
  reaches the row lock — a distinct failure mode from `lock_timeout`, which is why it's now its
  own retryable class above rather than silently surfacing as an unhandled exception instead of
  the documented `503`. Pool `min_size`/`max_size` should be set comfortably above the concurrency
  ceiling this project actually exercises — e.g. `max_size=120` against CT6's 100-attempt upper
  bound — with real headroom, not exactly matched to it.
- **Wallet existence is not a business-rule outcome (fixed on review — see ADR-0002's validation
  boundary).** `SELECT ... FOR UPDATE WHERE id = :wallet_id` returning zero rows means the wallet
  doesn't exist. This is discovered *inside* the transaction (locking is the only place that
  actually needs to look the row up) and *before* the transfer row is inserted (per the
  wallet-locking-order fix above), and it is handled by rolling back the **entire** transaction —
  idempotency-record insert included — and returning `404`, not by persisting a `FAILED` transfer.
  `transfers.from_wallet_id`/`to_wallet_id` being foreign keys (ADR-0004) still makes a transfer
  row referencing a nonexistent wallet unrepresentable as defense-in-depth; it's the explicit lock
  query, not that constraint, that this path actually relies on to find the wallet missing. A
  retry with the same key after this simply re-runs the same check and fails the same way — safe
  without needing a stored record, same reasoning as ADR-0002's other pure-validation failures.
- **Retry exhaustion.** If all bounded-retry attempts still hit `lock_timeout`/`deadlock_detected`,
  the entire transaction (idempotency record included) has rolled back every time — nothing is
  persisted. The API returns a distinct, retryable `503` rather than a generic error. Because
  nothing was persisted, the client can safely retry later with the *same* idempotency key — there
  is no conflicting record to collide with.
- The locked critical section (lock → balance check → ledger insert → status update → commit)
  contains no external calls or unrelated queries, to bound how long the locks are actually held.
- Isolation level: Postgres's default `READ COMMITTED` is sufficient. The row locks are what
  provide the safety property, not the isolation level — and plain reads (e.g. a balance query)
  are never blocked by these write locks under MVCC, so this doesn't cost read availability.
- **Empirical validation, not just unit tests:** `scripts/simulate.py` seeds wallets and drives
  the real running HTTP API through concurrent load — same-wallet fan-in, opposite-direction
  pairs, duplicate-key replay — reporting a pass/fail balance reconciliation. This is what ADR-0005
  refers to as this ADR's demo tooling; it exists specifically because the automated test suite's
  concurrency tests (below) prove correctness at test-scale, and this is the way to see the same
  guarantees hold under a heavier, more realistic load, doubling as the interview demo.

### Required test scenarios (critical section) — repository/integration tier, real Postgres, real concurrent connections

- **CT1** — N concurrent transfers debiting the same source wallet where combined amount exceeds
  its balance: exactly the affordable ones succeed, the rest resolve `FAILED`, final balance is
  correct and never negative.
- **CT2** — two concurrent transfers on the same wallet pair in opposite directions (`W1→W2` and
  `W2→W1` simultaneously): both complete, no deadlock, proving the ascending-id lock ordering.
- **CT3** — forced lock contention (hold a lock open deliberately in one test transaction): the
  contending transaction hits `lock_timeout`, the bounded-retry wrapper retries, and it succeeds
  once the first releases.
- **CT4** — two concurrent requests with the *identical* idempotency key (double-fire): exactly
  one transfer is created, both callers receive the same terminal result, no duplicate ledger
  entries.
- **CT5** — two concurrent requests with the *same* idempotency key but a *different* payload: the
  loser gets `409`, no transfer is created for it.
- **CT6** — high-concurrency fan-in stress (e.g. 50-100 concurrent attempts against one wallet, via
  `scripts/simulate.py`): final balance reconciles exactly against the expected value.
- **CT7** — a client-initiated retry (same idempotency key) is injected while the *original*
  request is between its own internal bounded-retry attempts — i.e. after one internal attempt
  has hit `lock_timeout` and rolled back (idempotency record included), before the next internal
  attempt begins. Needs an explicit synchronization point in the test (a hook fired right after
  the first attempt's rollback, not a wall-clock sleep) to land the injected retry deterministically
  in that gap, rather than relying on timing luck. Exactly one transfer is still created; both the
  original request (once it completes) and the injected retry receive the same terminal result; no
  duplicate ledger entries — proving the idempotency guarantee holds across an arbitrary
  interleaving of client- and server-initiated attempts on the same key, not just the
  two-top-level-requests case CT4 covers (ADR-0002).

## Consequences

**Good:** deadlock-safe by construction, not by convention. Predictable, bounded behavior under
contention instead of unbounded queuing. Deterministic enough to test directly: concurrent
same-wallet fan-in and concurrent opposite-direction pairs are both reproducible test scenarios
that either hold or don't, with no flaky timing dependence on retry semantics doing the real work.

**Bad / risks:** pessimistic locking serializes throughput **per wallet** — that's inherent to
this approach, not an oversight. A wallet under genuinely extreme load (thousands of TPS on one
wallet — a treasury/hub wallet, say) would eventually need a different architecture entirely
(partitioned/sharded balance, or an async ledger-first design with eventual consistency).
Recorded as a named ceiling in `ARCHITECTURE.md`'s Known Limitations once written, not solved
here — building for that scale now would be solving a problem this assignment doesn't have.
Bounded retry adds up to ~3x latency in the worst case under contention, which is the accepted
cost of failing safely and recoverably instead of failing on the first collision.
