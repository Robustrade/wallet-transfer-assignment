# Design

> The why. Read top to bottom — it should leave you with the model in your head.
> For mechanics, race walkthroughs, and alternatives considered, follow the
> `→ Deep dive:` links into [`DESIGN_DEEP_DIVE.md`](./DESIGN_DEEP_DIVE.md).

## Executive summary

`POST /transfers` moves money between two wallets. That sentence hides almost everything that matters. The interesting questions are: *what happens when the same request arrives twice?* *what happens when two requests target the same wallet?* *what happens when the database rolls back halfway through?* *what is the source of truth for a balance?*

The assignment asks for idempotency, concurrency safety, a double-entry ledger, and safe state transitions. Read that prompt closely and a tighter design falls out almost mechanically: a single transaction that claims an idempotency key, locks two wallets in deterministic order, writes one transfer row and two ledger rows, and updates two balances — all or nothing. Around that core sit a few small machines that handle the cases the core can't reach: a fresh transaction for recording failures (the core has rolled back, so it can't), a bounded retry loop for the conflict-then-rollback race, and a periodic sweep to keep the idempotency table from growing without bound.

That's the whole system. Everything below explains why each piece is the shape it is, and what it deliberately is not.

## §1 Reading the assignment

`ASSIGNMENT.md` lists requirements as a checklist: idempotent requests, double-entry ledger, correct balances, safe state transitions, concurrency safety. Treating the list as independent items is the trap — the requirements are *coupled*, and the coupling is where the design lives.

Three couplings drive everything else:

**Atomicity ⊗ Idempotency.** "Transfers must execute atomically" means the wallet UPDATE, the ledger INSERTs, and the transfer INSERT are one transaction. "Duplicates must not double-spend" means the idempotency claim must be in *that same transaction* — if the claim were in a separate prior transaction, an executing crash would leave a claimed key with no corresponding work, and the next retry would see the claim and return a stale or absent result. This forces the claim to be `INSERT ... ON CONFLICT DO NOTHING` inside the executing transaction. → §3, §7.

**Atomicity ⊗ Failure recording.** If the executing transaction rolls back (insufficient balance, missing wallet), the claim row goes with it. But the assignment says "the system must return the original result" on duplicates — including failures. So we need to write a FAILED record *after* the rollback, in a transaction that survives the failure of the first. That requires `REQUIRES_NEW` propagation and an `INSERT`-shaped (not `UPDATE`-shaped) primitive, because the row to update no longer exists. → §7.

**Concurrency ⊗ Determinism.** "Two transfers attempt to debit the same wallet simultaneously" is the example the assignment gives. The naive answer (`SELECT FOR UPDATE` on each wallet) is correct *only if* you lock them in deterministic order — otherwise A→B and B→A deadlock. The order has to be a property of the wallets, not of the request. We sort lexicographically on `wallet_id`. → §4.

The rest of the design is downstream of these three coupling decisions.

## §2 System shape

Five layers, each with a single responsibility:

```
HTTP filters     → tracing (MDC), idempotency-key gate, optional HMAC, body cache
Controllers      → header/body binding, status-code mapping
Services         → orchestration: TransferService → TransferExecutionService
Repositories     → jOOQ-typed SQL (no business logic)
Postgres         → integrity constraints, immutability triggers, advisory locks
```

`TransferService` is non-transactional and handles the things that *cannot* be inside the transaction: client validation, conflict resolution, retry-loop bookkeeping, FAILED-state recording. `TransferExecutionService` is the single `@Transactional` method — it owns the all-or-nothing core. The split is deliberate: putting retry logic inside `@Transactional` would let a retry happen inside a not-yet-committed transaction, which is incoherent. Putting client lookup inside the transaction would burn a connection on a 403-bound request. The boundary is the contract.

Database integrity invariants live in DDL, not in code: `balance >= 0` is a `CHECK` constraint, `from <> to` is a `CHECK` constraint, "ledger entries are immutable" is a `BEFORE UPDATE/DELETE` trigger, "wallets are append-only" is a `BEFORE DELETE` trigger. Application code never re-validates these. If the application is ever bypassed — a Flyway repair, a maintenance script, a future direct-SQL admin path — the database still says no. → Deep dive: [§2](./DESIGN_DEEP_DIVE.md#2-system-shape).

## §3 Idempotency

The obvious idempotency model is *check, then write*: read the row, if absent insert the result. Two concurrent requests with the same key both observe "absent" before either commits. Both proceed. We've doubled the side effects.

The right primitive is one statement that atomically claims and detects collision: `INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING`. Postgres returns 1 row affected on a fresh insert, 0 on a collision. The collision arm is the duplicate path.

But that alone is not enough. `INSERT ... ON CONFLICT DO NOTHING` blocks until the conflicting transaction commits or rolls back — which means a peer request mid-execution makes the second request *wait*, holding a connection, for the duration of the peer's transaction. Under contention this serializes a hot key behind one connection-time. So we add a transaction-scoped advisory lock as a fast-reject in front of the unique index: same hash → `pg_try_advisory_xact_lock` → false → reject with `409 In-Progress` immediately, no waiting. The advisory lock is best-effort (it lives in process memory, not durable storage), but the unique index is the durable correctness boundary. The two together give us low-latency rejection of in-flight retries plus durable exactly-once across process restarts.

A duplicate request after the original has *committed* lands on the unique index, returns 0 rows, and we look up the cached `(response_status, response_body)` and replay it. Replay is byte-for-byte: we ship the cached JSON straight to the wire instead of parsing it back through Jackson. → Deep dive: [§3](./DESIGN_DEEP_DIVE.md#3-idempotency).

## §4 Concurrency

`READ_COMMITTED` isolation, `SELECT ... FOR UPDATE` on both wallets in lexicographic ID order. Three choices, each over a tempting alternative:

- **`READ_COMMITTED` over `SERIALIZABLE`.** The threat model is double-spend on the same wallet, not write skew across unrelated rows. `SELECT FOR UPDATE` already serializes any pair of transactions touching the same wallet. `SERIALIZABLE` adds the cost of serialization-failure retries on transactions that don't actually conflict, which under load amplifies tail latency without buying anything for our workload.
- **Pessimistic over optimistic.** A version-column / `WHERE version = ?` UPDATE works, but turns every contended transfer into a retry storm — and our retry budget already exists at a different layer (§6). Two retry loops on the same operation make latency spikes much worse than one. Pessimistic locking pushes the wait into the database where Postgres can fairly schedule it.
- **Deterministic lock order.** Sort the wallet IDs lexicographically and acquire the first lock, then the second. Without this, A→B and B→A deadlock under any load. With it, the second of a pair simply waits for the first to commit. → Deep dive: [§4](./DESIGN_DEEP_DIVE.md#4-concurrency).

## §5 Ledger

Two pieces of the ledger that fall out of careful reading.

**Two entries per transfer, enforced by the schema, not by convention.** `ledger_entries` has `UNIQUE (transfer_id, wallet_id)`. A transfer cannot accidentally produce three rows; a duplicate insert violates the constraint and rolls back the transaction. The "double" in double-entry is a database invariant.

**The ledger is the source of truth; `wallets.balance` is a maintained projection.** Both writes happen inside the same transaction, so the projection cannot diverge from the ledger as long as that invariant holds. The `running_balance` we record on each ledger row is the post-update wallet balance returned by the UPDATE — taken from the persisted row, not the in-memory entity, so a future code reordering that lets the in-memory entity drift cannot corrupt the audit record.

**Immutability at the database.** `BEFORE UPDATE` and `BEFORE DELETE` triggers on `ledger_entries` raise an exception. There is no in-application path that can edit a posted entry, but more importantly there is no out-of-application path either: a maintenance script that tries to "just fix this one row" gets a clean exception. Reversing a transfer means posting a compensating transfer, not editing the rows. Wallets are similarly append-only via a `BEFORE DELETE` trigger — `WalletService.getTransferHistory` short-circuits its existence check on the assumption that "transfer rows imply the wallet exists," and the trigger pins that assumption at the only place that can enforce it. → Deep dive: [§5](./DESIGN_DEEP_DIVE.md#5-ledger).

## §6 Retries & error semantics

The interesting case isn't "request succeeds" or "request fails on its merits." It's "request claimed the key, then rolled back" — say a wallet was missing, or a constraint fired. The claim row is gone with the rollback. A peer retry that ran concurrently on the same key now sees an absent row and proceeds. This is *correct* — the prior attempt left no trace, so the retry is a fresh attempt — but it surfaces as `IdempotencyConflictException` from the executing service when the peer's `INSERT ... ON CONFLICT` returns 0.

`TransferService` resolves these by re-reading the record. If it's still absent, the prior attempt rolled back; we sleep with jittered backoff and retry. If it's present and matches our hash, we replay. If it's present and the hash differs, we surface `409 hash conflict` — a different payload reused the same key.

We bound this loop at three attempts. Beyond that, surface `503 Service Unavailable` with `Retry-After: 1`. The `503` is *not* the same as `409` — `409` is a caller bug ("you sent a different payload"), `503` is "system is contended." Conflating them would mislead any oncall reading a hot-shard incident dashboard. The full status-code matrix lives in [`docs/RETRY_CONTRACT.md`](./RETRY_CONTRACT.md), which this section deliberately does not duplicate.

Database timeouts (`statement_timeout=10s`, `lock_timeout=8s`, set in `connection-init-sql`) surface as `504`. They are server-side and retryable from the client's perspective; the caller's `(client, key)` claim either rolled back or never landed, so the retry is safe. → Deep dive: [§6](./DESIGN_DEEP_DIVE.md#6-retries--error-semantics).

## §7 Failure recording

When the executing transaction rolls back due to a business failure (insufficient balance, missing wallet), the claim row vanishes. We need a record that says "this key terminated in a failure" so duplicates return the cached error instead of re-executing.

Two choices fall out:

**`REQUIRES_NEW` propagation.** `IdempotencyFailureRecorder` opens a brand-new transaction. The outer rollback is already committed (so to speak — its absence is committed) by the time the recorder runs. The new transaction is independent.

**`INSERT`, not `UPDATE`.** A symmetric `markFailed` UPDATE on the IN_PROGRESS row was tempting and rejected: by the time the recorder runs, the IN_PROGRESS row has rolled back, so the UPDATE no-ops against an absent row and the failure silently never persists. The repository deliberately exposes no `markFailed` method. The only way to record a failure is `insertFailedRecord` — which is `INSERT ... ON CONFLICT DO NOTHING`, idempotent on `(client_id, key)`, and silently yields if a concurrent retry has already won the claim.

For insufficient-balance failures we also write a FAILED `Transfer` row for audit. Order matters: Transfer first, then idempotency claim. If the idempotency claim loses to a concurrent retry, we accept an orphan FAILED transfer row rather than the inverse — a cached `422` response with no audit row would be much worse. → Deep dive: [§7](./DESIGN_DEEP_DIVE.md#7-failure-recording).

## §8 Authentication

`X-Client-Id` is a *namespace* on the idempotency key, not authentication. Two clients can use the same `Idempotency-Key` value and they are independent claims. With HMAC enabled, `X-Client-Id` is also the lookup key for the shared secret. With HMAC disabled, `X-Client-Id` is unauthenticated and trivially forgeable.

HMAC is off by default. The application starts up loud — `INFO` line when on, `WARN` line when off, both grep-friendly for log-alerting pipelines. Enabling it in production is the single most important deployment switch. Off-by-default exists so the assignment can be exercised without provisioning a secret; the warning makes it impossible to ship the off configuration to production by accident.

The HMAC filter signs `method:path:clientId:idempotencyKey:timestamp:sha256(body)`. Including the body hash means a tampered payload changes the signature; including the timestamp gives a replay window (default 5 minutes) bounded by the client clock. Body caching is non-trivial — the filter reads the body for signing, the controller reads it again for JSON binding — and is handled by `CachedBodyHttpServletRequest` upstream of both. The signature filter asserts at runtime that the cached wrapper is in place; if filter ordering is ever broken, the assertion fails fast rather than confusing the next reader with "request body is missing." → Deep dive: [§8](./DESIGN_DEEP_DIVE.md#8-authentication).

**Known limitation: one global signing secret across all clients.** `SignatureFilter` verifies every HMAC against a single shared `security.signature.secret`, so any party holding that secret can mint a valid signature under any `X-Client-Id` — a cross-tenant attribution forgery (B's logs, B's idempotency namespace). The fix is a per-client config map plus a small resolver; the trade-off, the hazard, and the implementation sketch are pinned in [§8.11](./DESIGN_DEEP_DIVE.md#811-known-limitation-single-shared-signing-secret-across-clients). Required reading before any multi-tenant deployment.

## §9 Timeouts & observability

Timeouts are set at the database connection, not at the transaction. `connection-init-sql: "SET lock_timeout = '8s'; SET statement_timeout = '10s'"` runs on every Hikari connection acquisition. These are enforced *server-side by Postgres* — Spring's `@Transactional` timeout fires only at statement boundaries, which leaves a thread blocked on a row lock unbounded. The Postgres-level timeout fires regardless of where in the call stack the wait is happening: `statement_timeout` surfaces as SQLState `57014` (`query_canceled`), `lock_timeout` surfaces as SQLState `55P03` (`lock_not_available`). Both are mapped to `504 Gateway Timeout` by `GlobalExceptionHandler` — `55P03` via the typed `CannotAcquireLockException` handler, `57014` via the SQLState walk on `DataAccessResourceFailureException`.

The transaction's `@Transactional(timeout=10)` is a redundant ceiling on top of `statement_timeout`; both exist so a misconfiguration of either still bounds the request.

Metrics are Micrometer + Prometheus. The `transfers.execute.duration` histogram has an `outcome` tag — `success`, `replay`, `insufficient_balance`, `wallet_not_found`, `transient_conflict`, `hash_conflict`, `in_progress`, `unknown_client`, `error`. Alerting on `error` and `transient_conflict` is the most useful starting point; `replay` going up faster than `success` is a signal of client retry storms.

MDC tracing carries `correlationId` (from header or generated), `requestId` (per HTTP request), and `clientId`. Every log line — including framework logs from Hikari and jOOQ — carries this context. → Deep dive: [§9](./DESIGN_DEEP_DIVE.md#9-timeouts--observability).

## §10 Lifecycle

Idempotency records can't live forever. The `idempotency_records` table grows by one row per logical transfer attempt. Without eviction, a long-running deployment ingests rows monotonically until the table dominates Postgres memory and query plans degrade.

We evict by `first_seen_at` rather than `last_seen_at` so a frequently-replayed key cannot extend its lifetime indefinitely; retention has a hard ceiling. Default retention is 72 hours, which is also the implicit maximum useful "I lost my response, let me retry" window for clients.

The cleanup runs on a Spring `@Scheduled` cron (default hourly). Multi-replica deployments use leader election via a transaction-scoped advisory lock — `pg_try_advisory_xact_lock` — to make sure only one instance per tick does the work. Session-scoped advisory locks were rejected because Hikari can return different physical connections for the acquire and the release, leaving the lock pinned to a session for up to `max-lifetime` (30 min). Transaction-scoped releases automatically on commit/rollback, regardless of which connection runs the next statement. → Deep dive: [§10](./DESIGN_DEEP_DIVE.md#10-lifecycle).

## §11 What we deliberately did not build

A list of *no*s, each with the reason it isn't a yes.

- **Wallet ownership / per-wallet ACLs.** The assignment doesn't ask for them, and adding ownership without a clear identity model produces a worse approximation of authorization than not having one. With HMAC enabled, `X-Client-Id` is authenticated; with HMAC off it isn't. Either way, *who is allowed to move money between wallets A and B* is left to whatever sits in front of this service.
- **Multi-currency.** The schema is `BIGINT amount` — a single denomination. Adding currencies without an FX policy, rounding rules, and a settlement model is worse than not having it.
- **Async retry queue.** `503` with `Retry-After` is a synchronous contract with the client. An internal queue that re-runs failed transfers turns a stateless service into a stateful one and changes the operational model substantially.
- **Soft-delete on wallets.** The DB trigger blocks `DELETE`. A future "soft-delete" requires reconsidering the `getTransferHistory` short-circuit (§5) and probably a new column. It's a schema change, not a code patch.
- **Bulk transfers.** `POST /transfers` is single-transfer. Bulk would need a different concurrency model (lock all wallets touched by the bulk in one shot) and a different error contract (partial success).
- **A markFailed UPDATE.** Already covered (§7) — the row to update doesn't exist by the time failure runs.
- **A session-scoped advisory lock.** Already covered (§3, §10) — Hikari connection-rotation makes the release unreliable.

→ Deep dive: [§11](./DESIGN_DEEP_DIVE.md#11-what-we-deliberately-did-not-build).

---

## Glossary

| Term | Meaning here |
| --- | --- |
| Idempotency claim | The `INSERT ... ON CONFLICT DO NOTHING` into `idempotency_records` that turns the key into a row. |
| Advisory lock | Postgres-level cooperative lock keyed by a 64-bit integer. Used here as a fast-reject for concurrent in-flight retries. |
| Conflict-then-rollback race | Two requests with the same key; the first claims and then rolls back; the second observes the claim, gets a conflict, then sees the absent row on re-read. |
| Replay | A duplicate request after the original has committed. The cached `(status, body)` is shipped verbatim. |
| Hash conflict | Same `(client, key)` reused with a different payload. Surface as `409`. |
| Transient conflict | The retry budget for the conflict-then-rollback race exhausted. Surface as `503` + `Retry-After`. |
| Append-only | The DB rejects `DELETE` and (for ledger entries) `UPDATE` via a `BEFORE` trigger. |
