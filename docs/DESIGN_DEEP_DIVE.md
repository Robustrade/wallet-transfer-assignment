# Design — Deep Dive

> Reference material. Same section structure as [`DESIGN.md`](./DESIGN.md), but
> each section is mechanics + race walkthroughs + alternatives rejected +
> operational consequences + the tests that lock behaviour in. Not meant to be
> read top-to-bottom — open the section you need.
>
> Each section opens with `← Overview` linking back to the corresponding
> section in `DESIGN.md` so you can find the elevator-pitch version.

## Contents

1. [Reading the assignment](#1-reading-the-assignment)
2. [System shape](#2-system-shape)
3. [Idempotency](#3-idempotency)
4. [Concurrency](#4-concurrency)
5. [Ledger](#5-ledger)
6. [Retries & error semantics](#6-retries--error-semantics)
7. [Failure recording](#7-failure-recording)
8. [Authentication](#8-authentication)
9. [Timeouts & observability](#9-timeouts--observability)
10. [Lifecycle](#10-lifecycle)
11. [What we deliberately did not build](#11-what-we-deliberately-did-not-build)

---

## 1. Reading the assignment

← Overview: [`DESIGN.md` §1](./DESIGN.md#1-reading-the-assignment)

The assignment fits on one page. The interesting work is in figuring out what *isn't* on the page — the consequences each requirement forces on the design. This section walks through the prompt requirement-by-requirement, traces each one to its non-obvious implications, and ends with a small handful of decisions that we made *despite* the prompt being silent on them, because the silence itself was a signal.

### 1.1 The eight prompt clauses, decomposed

The assignment makes eight load-bearing claims about correctness. Each one has at least one non-obvious consequence.

**(a) "If the same `idempotencyKey` is used again, the system must return the original result."**

Naive reading: cache the first response, return it on second call. The trap is *concurrent* duplicates, not just sequential ones. Two requests with the same key arrive within a few milliseconds — both check the cache, both find it empty, both proceed. By the time either thinks to cache, the side effect has already doubled. So the deduplication primitive cannot be a check-then-write; it has to be a single statement that atomically *claims* the key and detects collision. That points at `INSERT ... ON CONFLICT DO NOTHING`, which is the load-bearing primitive in §3.

A second non-obvious consequence: "the original result" includes the original *failure*. If the first request returned `422 Insufficient balance`, the second must too — a retry that re-evaluates the balance might find a different answer because some other transfer has cleared in between, but per the prompt the second attempt is supposed to look like the first. That forces a cached-failure record (§7).

**(b) "Duplicate requests must not trigger duplicate transfers."**

This is the strong form of (a). Even if the system *crashes* between claiming the key and writing the transfer, the next retry must not produce a second transfer. The only way to achieve that is to put the claim and the work in the *same transaction* — a separate "claim then execute" pattern would let a crash strand a claim with no work, and the retry would see the claim and skip the transfer that was never written.

This is the coupling between idempotency and atomicity. It rules out a whole family of designs that store the idempotency key in an external store (Redis, in-memory map, sidecar service) — those decouple the claim lifecycle from the work, and the only way to put them back together is two-phase commit, which is dramatically more complexity than the requirement justifies.

**(c) "Transfers must execute atomically."**

The five state changes inside one transfer — INSERT transfer, UPDATE source wallet, UPDATE dest wallet, INSERT debit ledger entry, INSERT credit ledger entry — must all commit or all roll back. Combined with (b), that means the idempotency claim is also inside that transaction. Combined with (a)'s failure-caching requirement, that means we need a *separate* transaction for failure records, because the executing transaction has rolled back. That's where `REQUIRES_NEW` (§7) comes from. It's not a Spring trick we wanted to use — it's the only thing that satisfies the constraint set.

**(d) "Every transfer must produce two ledger entries."**

The interesting part is "every transfer." Not "the application always inserts two rows" — that's a code property that any future refactor can break. Pin it in the schema: `UNIQUE (transfer_id, wallet_id)` on `ledger_entries`. A future code path that tries to write three rows for the same transfer gets a constraint violation and the whole transaction rolls back. The "two" is a database invariant, not a coding convention.

This same idea — "make the database say no" — repeats throughout the design. It's the thread that runs through the schema decisions in §5.

**(e) "Transfers should have a state machine. PENDING → PROCESSED, PENDING → FAILED."**

Two reads. The literal read is "model the states explicitly," which we do (`status VARCHAR(16) CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))`). The deeper read is *what does PENDING mean here?* In a system with async settlement (queue, message bus, reconciliation), PENDING is a real state that lives between request acknowledgment and final settlement. In our system, the entire transfer is one synchronous transaction — there is no asynchronous gap to model. So we INSERT directly in PROCESSED state on the happy path, and directly in FAILED state on the audit-recording path; PENDING exists in the schema for future-proofing but is not a state the application ever writes.

This is a place where a careful read changes the design *toward simplicity*. A naive implementation would write PENDING then UPDATE to PROCESSED, which is wasted work in a synchronous world.

**(f) "The system must ensure correct balances, no double spending, consistent ledger entries [under concurrent requests]."**

The prompt uses the word "ensure" — not "minimize," not "best effort." That word carries the weight. It rules out optimistic concurrency as the *only* line of defence: optimistic schemes detect conflicts and retry, which is a probabilistic guarantee on success time, not a guarantee on correctness. To "ensure," we need pessimistic locking on the rows that double-spend would touch — the wallet rows. That's `SELECT FOR UPDATE`. The rest of §4 is consequences of that choice.

**(g) "Duplicate requests, retries, network failures, partial execution."**

Each of these is a case the system must handle, not just survive. Partial execution is the most interesting: what happens when the request reached the server, the server began processing, and then the *connection* dropped? The server doesn't know whether to commit or roll back from the network's perspective; the transaction proceeds on its own timeline. The client sees a timeout and retries. The retry must:

- not double-spend (handled by the unique idempotency claim, §3)
- find any prior progress and replay it (handled by the cached response, §3)
- not deadlock with any in-flight peer (handled by deterministic lock order, §4)
- not be silently swallowed if the prior attempt was rolling back at the moment of retry (handled by the bounded retry loop in `TransferService`, §6)

The fourth one is the conflict-then-rollback race, which has its own dedicated subsystem because none of the other primitives cover it.

**(h) "Choose and justify your strategy."**

Read literally: write your reasoning down. We did, in this document and the inline Javadoc. The implicit invitation is "show that you considered alternatives and rejected them for reasons" — every section below has a *Rejected* subsection.

### 1.2 What the prompt is silent on, and what we did about it

The prompt is silent on six things that careful design has to cover anyway. Each of these is an explicit decision that the assignment doesn't require but that the system would be incorrect or fragile without.

1. **Authentication.** The prompt mentions `idempotencyKey` but not `clientId`. We added `X-Client-Id` because without it, idempotency keys collide across tenants — `Idempotency-Key: abc123` from caller A and the same value from caller B would alias to the same record. Scoping the key by `(client_id, idempotency_key)` makes it tenant-safe. HMAC signing is layered on top as an opt-in; off by default with a loud `WARN` on startup. (§8)
2. **Wallet ownership.** Who is allowed to move money between which wallets? The prompt doesn't say. We deliberately did not invent an answer — wallet ownership without a clear identity model is worse than no answer at all (§11). Whoever puts this service behind their own gateway gets to decide.
3. **Currency / FX.** `BIGINT amount`, single denomination. Multi-currency without FX rates and rounding rules is a footgun.
4. **Per-request authorization.** Same as wallet ownership.
5. **Bulk transfers.** `POST /transfers` is single-transfer. A bulk variant would need a different concurrency model and a different error shape (partial success). Out of scope.
6. **Retention.** The idempotency table can't grow forever, but the prompt doesn't ask. We added a 72-hour rolling cleanup with leader election (§10) because the table grows monotonically without it.

### 1.3 The three couplings that drive the rest

Distilling the above to the three coupling decisions that drive most of the design:

| Coupling | Consequence |
| --- | --- |
| Atomicity ⊗ Idempotency | The idempotency claim must be in the executing transaction. → §3 |
| Atomicity ⊗ Failure recording | Failure must be recorded in a *different* transaction. → §7 |
| Concurrency ⊗ Determinism | Wallet locks must be acquired in deterministic order. → §4 |

Every other decision in this document is downstream of these three.

### 1.4 Where this gets tested

The "did we read the assignment carefully" claim is testable. The integration test suite asserts each prompt clause directly:

- (a) and (b) → `IdempotencyIntegrationTest.shouldReturnSameResultForDuplicate`, `shouldNotDeductBalanceOnReplay`
- (c) → `TransferIntegrationTest.shouldFailWithInsufficientBalance` and `LedgerIntegrationTest.shouldNotCreateLedgerEntriesForFailedTransfer` (no partial state survives)
- (d) → schema-level `UNIQUE (transfer_id, wallet_id)` is itself a permanent test
- (e) → state-transition tests check we never write PENDING and never reach FAILED → PROCESSED
- (f) → `ConcurrencyIntegrationTest.shouldPreventOverdraftUnderConcurrency` runs 10 parallel debits against the same wallet (5 must succeed, 5 must fail with insufficient balance, final balance is exactly 0)
- (g) → retry / conflict-handling tests use real Testcontainers Postgres with `READ_COMMITTED`

Anything that breaks one of the prompt clauses fails a test. Anything subtler that breaks an *implication* of a prompt clause is the harder case — that's what the rest of this document is about.

---

## 2. System shape

← Overview: [`DESIGN.md` §2](./DESIGN.md#2-system-shape)

The application is a single Spring Boot process. Its internals are a five-layer stack with one rule: each layer can only call *down*, never up or sideways. This section walks through each layer, the responsibilities it owns, and — more importantly — what it *isn't* allowed to do.

### 2.1 Request flow at a glance

```
┌─────────────────────────────────────────────────────────────────────┐
│  HTTP request                                                       │
└───────────┬─────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Filter chain (jakarta.servlet, ordered)                            │
│  ─────────────────────────────────────                              │
│   1. TracingFilter            — MDC: correlationId, requestId,      │
│                                 clientId; echo IDs to response      │
│   2. IdempotencyHeaderFilter  — require header on /transfers,       │
│                                 wrap body in CachedBodyHttp...      │
│   3. SignatureFilter          — HMAC verify (if enabled)            │
└───────────┬─────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Controller (TransferController / WalletController)                 │
│   - bind headers + body via @Valid                                  │
│   - delegate to service                                             │
│   - pattern-switch on the sealed TransferResult to choose the       │
│     wire shape (Jackson serialise vs raw bytes)                     │
└───────────┬─────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│  TransferService  (non-transactional)                               │
│   - reject unknown clients                                          │
│   - bounded retry loop                                              │
│   - resolveConflict: replay vs hash-mismatch vs absent              │
│   - failure recording delegated to IdempotencyFailureRecorder       │
└───────────┬─────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│  TransferExecutionService  (@Transactional, READ_COMMITTED, 10s)    │
│   - guardConcurrentRequest (advisory lock)                          │
│   - claimIdempotencyKey (INSERT ... ON CONFLICT)                    │
│   - lockBothWallets (SELECT FOR UPDATE in lex order)                │
│   - executeBalancedTransfer (5 statements, all-or-nothing)          │
│   - finalizeIdempotency (cache response on the row)                 │
└───────────┬─────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Repositories (jOOQ)                                                │
│   - typed SQL only, no orchestration, no business rules             │
└───────────┬─────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Postgres                                                           │
│   - CHECK / UNIQUE / FK constraints                                 │
│   - BEFORE UPDATE/DELETE triggers (ledger immutability,             │
│     wallet append-only, updated_at touchers)                        │
│   - lock_timeout 8s, statement_timeout 10s on every connection      │
└─────────────────────────────────────────────────────────────────────┘
```

Two things to notice:

- The `TransferService` / `TransferExecutionService` split is the only place where a "service" calls another "service." That's not a layer violation — the two have *different transaction semantics*, and merging them would force one of them to give up its semantics. The split is the contract.
- Postgres is a layer in the diagram. The integrity invariants there (CHECK, UNIQUE, triggers) are a *code-equivalent* defence against bugs above. If the application is bypassed, the database still says no.

### 2.2 The transactional split

The split between `TransferService` and `TransferExecutionService` is the load-bearing piece of the service layer. It exists because three things must happen on different transactional timelines:

| Operation | Transaction |
| --- | --- |
| Client existence check | None (a single SELECT, no need) |
| Transfer execution | A new transaction, `READ_COMMITTED`, 10s timeout |
| Failure recording | A *different* new transaction (`REQUIRES_NEW`) |
| Retry loop bookkeeping | Outside any transaction |

If `TransferService` were `@Transactional`, the retry loop would be inside an open transaction. Each retry would re-enter `tryExecuteTransfer`, which is also `@Transactional` — Spring would either nest (which Postgres does not really support without savepoints) or join the outer transaction (which would mean the retry happens inside the failed prior transaction, which is incoherent).

If `TransferExecutionService` were absent and the controller called the repository directly, every line of orchestration logic would be inside one giant `@Transactional` method, with retries, client validation, and failure recording all sharing the same lifecycle as the SQL writes. That's the design that breaks under the conflict-then-rollback race (§6) — there is no place for a retry to live that isn't inside the transaction it's retrying.

The Spring proxy mechanism that makes `@Transactional` work also imposes a constraint readers should know: **self-invocation does not trigger the proxy.** A method on `TransferService` calling another method on `TransferService` via `this.foo()` bypasses the proxy and the inner method's `@Transactional` annotation does nothing. We rely on this property only by *not* doing it; the only `@Transactional` boundary is the cross-bean call from `TransferService` to `TransferExecutionService`. The same constraint comes back in §10 (cleanup service) where `TransactionTemplate` is used instead of an annotation for the same reason.

### 2.3 Filters: ordered by precedence, gated by path

Three filters run on every request, in this order:

1. **`TracingFilter`** (`HIGHEST_PRECEDENCE`). Populates MDC with `correlationId` (from `X-Correlation-Id` header or generated UUID), `requestId` (always generated), and `clientId` (from `X-Client-Id` if present). Echoes both IDs back in response headers. Clears MDC in `finally` so a thread that returns to the pool cannot leak context into the next request.
2. **`IdempotencyHeaderFilter`** (`HIGHEST_PRECEDENCE + 10`). Gates `POST /transfers` on the presence and length (≤ 256 chars) of `Idempotency-Key`. Wraps the request in `CachedBodyHttpServletRequest` so the body can be read more than once (the signature filter and the controller both need it).
3. **`SignatureFilter`** (`HIGHEST_PRECEDENCE + 20`). When `security.signature.enabled=true`, verifies the HMAC. Otherwise no-op.

Path matching in the gate is normalized — lowercase, collapse `//+`, strip trailing `/` — to defend against bypasses like `/Transfers`, `//transfers`, or `/transfers/`. Without that, a misconfigured load balancer or a client that follows different URL conventions could route a real transfer past the idempotency gate. The normalization is a small cost on every request (one regex match) for a permanent class of vulnerabilities closed.

### 2.4 Where business rules live

Three places. Each has a different reason.

**Bean Validation (JSR-303) on the request DTO.** Field-level constraints (`@NotBlank`, `@Positive`, length caps) and the cross-field same-wallet rule (`@AssertTrue`) sit on `CreateTransferRequest`. Triggered by `@Valid` on the controller method. Surface as `MethodArgumentNotValidException` → 400 via `GlobalExceptionHandler.handleValidation`. The DTO knows nothing about the rest of the system, but every request that reaches the service has already passed validation.

**Service-layer guards.** Things that need data, not just shape: client existence (`UnknownClientException` → 403), wallet existence (`WalletNotFoundException` → 404), balance sufficiency (`InsufficientBalanceException` → 422). These need a database read or a domain operation; they cannot live in the DTO.

**Database constraints.** The invariants that *must* hold even if the application has a bug. `balance >= 0`, `from <> to`, `amount > 0`, `UNIQUE (transfer_id, wallet_id)` on the ledger, `BEFORE UPDATE/DELETE` triggers on `ledger_entries`, `BEFORE DELETE` trigger on `wallets`. None of these are re-checked in code. A `CHECK (balance >= 0)` violation surfaces as `DataIntegrityViolationException` and the transaction rolls back — which is exactly the right behaviour, because the only way this violation can fire is a bug in `executeBalancedTransfer`, and rolling back is safer than coding a graceful path through a logic error.

### 2.5 Repositories: typed SQL, no logic

The repositories are jOOQ-only. Each method is a single SQL statement (or a small composition for the `deleteOlderThan` IN-subquery). No method "decides" anything; the decisions are upstairs.

The repository names encode intent: `insertIdempotencyRecord` *claims*, `markCompleted` *transitions IN_PROGRESS → COMPLETED*, `insertFailedRecord` *inserts a fresh FAILED row*, `touchReplay` *bumps the replay counter*. There is intentionally no `markFailed` UPDATE method — see §7 for why.

jOOQ codegen runs from `src/main/resources/db/migration/*.sql` at build time. The DDL is the source of truth; the generated classes are derived. Adding a column means writing a new migration and re-running codegen, never editing the generated classes by hand.

### 2.6 What happens when the layers are bypassed

A useful exercise: assume each layer is bypassed in turn. What still holds?

- **Filters bypassed.** No tracing context, no HMAC. The controller still requires `Idempotency-Key` via `@RequestHeader` (so a missing header surfaces as `MissingRequestHeaderException` → 400), but the length cap is gone.
- **Controller bypassed (somehow).** Bean Validation no longer runs. The service still rejects unknown clients (403), missing wallets (404), and insufficient balance (422). The DB still rejects negative amounts and same-wallet via `CHECK`.
- **Service bypassed.** All invariants are now on the DB. `from <> to` is `CHECK`, `amount > 0` is `CHECK`, idempotency uniqueness is `PRIMARY KEY (client_id, idempotency_key)`, ledger uniqueness is `UNIQUE (transfer_id, wallet_id)`, ledger immutability and wallet append-only are triggers. The DB's "no" is louder and uglier than the service's, but it's correct.
- **Repository bypassed.** Means someone is hand-running SQL. Even then, the DDL says no.

The point of this exercise is not paranoia. It's that *correctness should not depend on the application being correct.* Every layer adds a faster, friendlier rejection of bad input, but the bottom layer is the one that has to be right.

### 2.7 Tests that lock this in

- `BaseIntegrationTest` spins a real Postgres via Testcontainers, so the `CHECK` and trigger behaviour is exercised on every integration test, not mocked.
- `IdempotencyHeaderFilterTest.shouldRequireKeyForCaseVariant`, `shouldRequireKeyForDoubleSlashVariant`, `shouldRequireKeyForTrailingSlashVariant` exercise the path-normalization (`/Transfers`, `//transfers`, `/transfers/` all enforce the gate).
- `TransferIntegrationTest.shouldReturnTracingHeaders` asserts both `X-Correlation-Id` (echoed unchanged from the request) and `X-Request-Id` (a fresh UUID) are present on the response.
- `LedgerIntegrationTest.shouldEnforceLedgerImmutability` directly attempts both `UPDATE` and `DELETE` on a posted ledger entry and asserts both raise `DataAccessException` (the trigger fires).

---

## 3. Idempotency

← Overview: [`DESIGN.md` §3](./DESIGN.md#3-idempotency)

This is the most subtle subsystem in the service. Idempotency in a transactional system is *not* "remember the response and replay it" — that one-line description hides at least four distinct races, each requiring a different primitive. This section walks through the model, the primitives, the races they cover, and the alternatives we considered for each.

### 3.1 The model

The contract is: for any tuple `(clientId, idempotencyKey)`, the API behaves as if exactly one logical request was processed. Re-issuing the same key — whether milliseconds, seconds, or hours later — yields a response *indistinguishable in side effects* from the first.

The implementation is a row in `idempotency_records`:

```
PRIMARY KEY (client_id, idempotency_key)
status         IN_PROGRESS | COMPLETED | FAILED
request_hash   SHA-256 of the canonical request
transfer_id    FK to transfers (set on COMPLETED and on insufficient-balance FAILED)
response_status INT       (200/201/422/etc., set on terminal states)
response_body   TEXT JSON (set on terminal states; replayed verbatim)
times_seen     INT        (incremented on every replay; observability only)
first_seen_at  TIMESTAMPTZ (used for retention)
last_seen_at   TIMESTAMPTZ (touched on every replay)
```

The lifecycle is tiny: `IN_PROGRESS → COMPLETED` (success) or `IN_PROGRESS → (rollback) → FAILED` (business failure). There is no `IN_PROGRESS → FAILED` UPDATE because the IN_PROGRESS row is gone by the time failure is recorded — see §7.

### 3.2 The two-tier lock

Two primitives guard a fresh execution:

**Tier 1: `pg_try_advisory_xact_lock`.** A 64-bit-integer cooperative lock, transaction-scoped, non-blocking. We hash `(clientId + ":" + idempotencyKey)` to a 64-bit lock id and try to acquire it. If another transaction in this Postgres instance already holds it, we get `false` immediately and return `409 In-Progress` without waiting.

**Tier 2: `INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING`.** Atomic claim against the durable index. Returns 1 row affected on a fresh insert, 0 on a conflict. If 0, we throw `IdempotencyConflictException`, which the outer `TransferService` resolves (§6).

Why both? Because each alone has a hole.

**The advisory lock alone.** It's process-local memory. Cross-instance peers can't see it. Two replicas under a load balancer would each acquire their own copy of the lock, and both would proceed. The lock is *fast* and *non-blocking*, but not *durable*.

**The unique index alone.** It's durable, but `INSERT ... ON CONFLICT DO NOTHING` *waits* for the conflicting transaction to finalize before reporting the conflict. That serializes a hot key behind one peer's transaction time. Worse, the waiting transaction holds a connection from the Hikari pool; under heavy contention, the pool can drain.

Layering them gives both properties: the advisory lock fast-rejects in-flight retries (no waiting, no pool pressure) and the unique index handles cross-instance correctness and durability. The advisory lock can have false-positive collisions because the hash folds to 64 bits — `Hashing.toLockId` collisions across unrelated keys would surface as a spurious `409`. We instrument that with `idempotency.advisory_lock.rejections`; under low traffic, a non-zero rate is the canary that says "investigate hash collisions." Durable correctness is preserved by the unique index regardless.

### 3.3 The four races

#### Race A — Two requests, same key, same payload, both arrive concurrently

```
T0: Request 1 begins                Request 2 begins
T1: R1 advisory lock acquire (✓)    R2 advisory lock acquire (✗ → 409 In-Progress, returned)
T2: R1 INSERT idempotency (✓)
T3: R1 lock wallets, debit/credit, write ledger
T4: R1 markCompleted, commit (✓)
T5:                                 (R2 client retries with same key)
T6:                                 R2 advisory lock acquire (✓ — R1 has committed)
T7:                                 R2 INSERT idempotency (✗ — unique key conflict)
T8:                                 R2 reads the row, hash matches, replays cached body
```

The advisory lock saves R2 from blocking on the unique index during R1's transaction. By the time R2 retries (after the `409 In-Progress`), R1 is done and the replay path is clean.

#### Race B — Two requests, same key, different payload

```
T0: R1 begins                       R2 begins
T1: R1 advisory lock (✓)            R2 advisory lock (✗ → 409 In-Progress)
T2: R1 INSERT (hash_A) ✓
T3: R1 commits
T4:                                 R2 retries (advisory lock now free)
T5:                                 R2 INSERT (hash_B) ✗ (unique conflict)
T6:                                 R2 reads row, hash_B ≠ hash_A → 409 Hash Conflict
```

The hash check is the second tier of the dedup model. Same key, different payload is a programmer error — likely a client bug that's substituting amounts or wallet IDs. It must not silently replay the original transfer. `409` with a clear message is the right answer; `200` with the original (unrelated) response would be much worse.

#### Race C — Replay after commit (the simple duplicate)

```
T0: R1 begins, claims, commits
... (any amount of time later)
T1: R2 begins with same (clientId, key), same payload
T2: R2 advisory lock (✓ — nothing in flight)
T3: R2 INSERT (✗ — durable claim from R1)
T4: R2 reads row, hash matches → replay cached body
T5: R2 touchReplay: times_seen += 1, last_seen_at = now()
```

The replay path is the common case in production — a client whose response was lost in transit retries; the transfer already committed; we ship the cached `(status, body)`.

A subtle property: the cached `response_body` is JSON written by `ObjectMapper.writeValueAsString(...)` at success time. On replay, we ship the bytes verbatim — the controller wraps the path in a sealed `TransferResult.Replay` variant whose `rawJson` is written with `byte[]` + `application/json`, bypassing Jackson on the way out. Re-parsing through Jackson would burn CPU on every replay for no semantic gain. That's not a micro-optimization; under a client retry storm, the duplicate path is the hot path.

The `201 → 200` status downgrade on replay is intentional: the original was a creation, the replay is not. A cached `201` would mislead a downstream that uses status code as "did this side effect just happen?"

#### Race D — Conflict-then-rollback (the hard one)

```
T0: R1 begins, claims, starts work
T1:                                 R2 begins with same key
T2:                                 R2 advisory lock (✗ → 409 In-Progress, returned)
T3: R1 hits InsufficientBalanceException, transaction rolls back
   (R1's IN_PROGRESS row is gone)
T4: R1 emits 422, then in TransferService catch:
    failureRecorder.recordInsufficientBalanceFailure (REQUIRES_NEW)
T5:                                 (R2 client retries the 409)
T6:                                 R2 advisory lock (✓)
T7:                                 R2 INSERT
   ─ if R1's REQUIRES_NEW failure record has committed: ✗ (claim by R1's recorder)
   ─ if not yet: ✓ (R2 takes the claim, executes fresh)
```

This is the race that justifies `TransferService`'s retry loop. R2 might INSERT successfully (the rollback is published, no row exists, no peer is in flight) — or might lose to R1's recorder if the timing puts R1's REQUIRES_NEW commit just ahead of R2's INSERT. Either outcome is correct; the retry loop tolerates both.

The retry loop (§6) handles a related variant: R2's INSERT *fails* with `IdempotencyConflictException`, then R2 re-reads the row to figure out what happened — `null` (peer rolled back, retry), present-with-matching-hash (replay), present-with-mismatching-hash (`409`).

### 3.4 The advisory lock collision question

`Hashing.toLockId` folds an arbitrary string to a 64-bit `long`. Two unrelated `(clientId, key)` pairs can collide. Under collision, two unrelated requests would each see the other holding the lock and one would get a spurious `409 In-Progress`.

We accept this risk explicitly. The reasoning:

- 64 bits is wide enough that the collision rate is negligible at human-scale traffic. Birthday paradox crosses 50% probability around 2³² items, ≈ 4 billion in-flight unique keys *at the same instant* across the cluster.
- Durable correctness is preserved by the unique index — a collision causes a spurious `409` (retryable from the client) but never a double-spend.
- `idempotency.advisory_lock.rejections` is tagged in the metric. A sustained non-zero rate at low traffic is investigable.

### 3.5 The replay counter and timestamps

`times_seen` is incremented server-side via `SET times_seen = times_seen + 1` (not application-side). Under `READ_COMMITTED`, two concurrent UPDATEs on the same row serialize via row lock; the second waiter re-reads the latest committed value before evaluating the SET. Increments are linearized; counter is exact.

`last_seen_at` is touched on every replay; `first_seen_at` is set once on insert and is the field we filter on in cleanup (§10), giving frequently-replayed keys a hard retention ceiling.

The integration test `IdempotencyIntegrationTest.shouldIncrementTimesSeenOnReplay` asserts the counter increments correctly. A subtle test detail: it does *not* sleep between requests to "ensure clock advance," because Windows clock resolution (~16ms) makes that flaky on slow CI. Instead, the assertion is `lastSeenAt isAfterOrEqualTo` (monotonic non-decrease) and the strict happens-after evidence is the counter value, which is independent of clock resolution.

### 3.6 Alternatives rejected

**Check-then-insert.** Read the row; if absent, insert; otherwise replay. Broken for concurrent duplicates: two requests both see "absent" before either commits. Lots of production systems have shipped this and discovered the bug at 3am. We use `INSERT ... ON CONFLICT DO NOTHING` precisely because it folds the check and write into one atomic statement.

**Session-scoped advisory lock with explicit unlock.** `pg_try_advisory_lock(...)` paired with `pg_advisory_unlock(...)` in a `finally` block. Rejected because Hikari can return different physical connections for the acquire and the unlock — the unlock would be a no-op against a connection that doesn't hold the lock, and the lock would stay pinned to its original session for up to `max-lifetime` (30 min). The transaction-scoped variant releases automatically on commit/rollback regardless of which connection runs next.

**External idempotency store (Redis, etc.).** Decouples the claim from the work. To re-couple them you need a two-phase commit between Redis and Postgres, or you accept windows where the claim exists in Redis but the Postgres work doesn't (or vice-versa). The implementation cost is dramatically higher and the failure modes are harder to reason about. Postgres is already a reliable distributed store; using a second one for this is unjustified.

**Optimistic versioning on the idempotency record.** Adds a `version` column and uses `WHERE version = ?` on every UPDATE. Useless here — we don't UPDATE the row in a contended way; we INSERT it. The relevant concurrency primitive is uniqueness, not version.

**Storing the request body verbatim instead of a hash.** Storing the body would let us replay it on demand or do exact-match dedup. The request body is up to a few hundred bytes for our domain — not large, but not free. Storing the SHA-256 hash is 64 hex chars, compares in microseconds, and gives us strong guarantees against payload mutation. The full body is reconstructable from `(transfers, ledger_entries)` if anyone ever needs it.

### 3.7 Operational consequences

- **Replay storms inflate `times_seen` but cost almost nothing.** A `SELECT` + `UPDATE` against an indexed row, no business logic. Hot-path SLO.
- **`409 In-Progress` rates are an in-flight-retry signal.** Non-zero is normal under load; a spike is a client misbehaviour signal (retry without backoff).
- **`409 hash-conflict` rates are a client bug signal.** Treat any nonzero rate as actionable — it means a real client is sending different payloads under the same key.
- **Idempotency records are evicted at 72h** (§10). Replays after eviction execute as fresh requests. Tune the retention if your callers retry on a longer horizon.

### 3.8 Tests that lock this in

- `IdempotencyIntegrationTest.shouldReturnSameResultForDuplicate` — same key + payload returns the cached body.
- `IdempotencyIntegrationTest.shouldConflictWhenKeyReusedWithDifferentPayload` — Race B.
- `IdempotencyIntegrationTest.shouldConflictWhenKeyReusedWithDifferentWallets` — Race B variant.
- `IdempotencyIntegrationTest.shouldNotDeductBalanceOnReplay` — replays don't re-execute.
- `IdempotencyIntegrationTest.shouldReplayFailedTransferCorrectly` — failed-replay returns the cached failure.
- `IdempotencyIntegrationTest.shouldIncrementTimesSeenOnReplay` — counter is exact.
- `IdempotencyIntegrationTest.shouldReturnIdenticalErrorResponseOnReplay` — failure replay is byte-equal except for `timestamp`.
- `IdempotencyIntegrationTest.shouldAllowLongIdempotencyKey` — keys up to 256 chars work.
- Concurrent race A (claim collision while peer is in-flight) is exercised in `IdempotencyKeyInProgressIntegrationTest.shouldReturn409WhenAdvisoryLockIsHeldByPeer` — a peer session holds `pg_advisory_lock` on the derived lock id, the request thread must reject 409 immediately rather than block, succeed, or 5xx.
- Concurrent race D (parallel duplicates resolving cleanly) is exercised in `ConcurrencyIntegrationTest.shouldHandleConcurrentDuplicateRequests` — 5 parallel requests with the same key resolve into exactly one 201 plus a mix of 200 (replay) and 409 (in-progress sibling), every 2xx body referencing the same transfer id.

---

## 4. Concurrency

← Overview: [`DESIGN.md` §4](./DESIGN.md#4-concurrency)

The classical concurrency hazard in a wallet system is *double-spend*: two transfers debit the same wallet, both observe an unstale balance, both proceed, and the wallet ends up below zero (or below what the running ledger says it should be). The defence has three pieces: pessimistic locking on the wallet rows, deterministic lock order to prevent deadlock, and a `READ_COMMITTED` isolation level chosen for what it *doesn't* do as much as for what it does.

### 4.1 The threat model

Three races to defend against:

| Race | Concrete shape |
| --- | --- |
| Concurrent debits | `wallet_X` has 100. Two transfers arrive: X→A 80, X→B 80. Both transactions read balance=100, both pass the sufficiency check, both attempt to debit. |
| Reverse-direction deadlock | Transfer X→Y locks X, then waits for Y. A concurrent Y→X locks Y, then waits for X. Cyclic wait, deadlock. |
| Lost update | Transfer X→Y reads X.balance, computes new balance, writes. A concurrent UPDATE that ran in between is overwritten by the stale-read computation. |

Pessimistic locking (`SELECT ... FOR UPDATE`) handles race 1 and race 3. Deterministic lock order handles race 2. Each is independent; both are necessary.

### 4.2 The locking primitive

`SELECT ... FOR UPDATE` on the wallet row inside the transfer transaction. Postgres takes a row-level exclusive lock; any concurrent transaction that tries to lock the same row blocks until the holder commits or rolls back. The blocking is fair (FIFO at the lock manager) and bounded by `lock_timeout=8s` set in `connection-init-sql` — a wait beyond 8 seconds surfaces as SQLState `55P03` (`lock_not_available`) → mapped to `504` by `GlobalExceptionHandler`.

Two wallets per transfer means two locks. The order is computed before either acquisition:

```java
String firstId, secondId;
if (request.fromWalletId().compareTo(request.toWalletId()) < 0) {
    firstId  = request.fromWalletId();
    secondId = request.toWalletId();
} else {
    firstId  = request.toWalletId();
    secondId = request.fromWalletId();
}

Wallet first  = walletRepository.findByIdForUpdate(firstId)
        .orElseThrow(() -> new WalletNotFoundException(firstId));
Wallet second = walletRepository.findByIdForUpdate(secondId)
        .orElseThrow(() -> new WalletNotFoundException(secondId));
```

The order is lexicographic on `wallet_id`. It's a property of the *wallets*, not the *request*, which is the load-bearing point — both X→Y and Y→X compute the same `(firstId, secondId)` pair. Either both transactions try to acquire X first, in which case the second waits cleanly; or both try to acquire Y first, same outcome. There is no cycle.

A subtle point: the `findByIdForUpdate` returns `Optional`; we throw `WalletNotFoundException` if either wallet is missing. The throw rolls the transaction back, including the IN_PROGRESS idempotency claim — which means a missing wallet falls into the conflict-then-rollback race (§3.3 Race D), and the retry loop will record a FAILED row via `IdempotencyFailureRecorder.recordWalletNotFoundFailure` (§7). Note that we *do* throw inside the transaction; we *do not* try to handle the missing-wallet case before locking, because doing so would either need a separate read (extra round trip) or a CTE-based "lock if exists" (jOOQ-unfriendly, harder to read).

### 4.3 The isolation choice

`@Transactional(isolation = Isolation.READ_COMMITTED, timeoutString = "${transfer.transaction.timeout-seconds:10}")`.

`READ_COMMITTED` is the Postgres default. Three reasons we kept it:

**The wallet lock already serializes the only contention that matters.** Once both wallets are `FOR UPDATE`-locked, no other transaction can read-modify-write them. The lock is the serialization point. Higher isolation would add more, but the more isn't buying anything because the lock already covers the case.

**`SERIALIZABLE` would inflate spurious retries.** Postgres' SSI (Serializable Snapshot Isolation) detects "could two transactions have produced an order-dependent outcome had they run sequentially?" and aborts one with `40001` (`serialization_failure`). For our workload — independent transfers between different wallet pairs — the answer is almost always "no, they don't interact," but SSI's predicate locking can flag false positives under load. Each false positive is a retry, and we already have a retry loop for the conflict-then-rollback race; doubling up the retry mechanism amplifies tail latency without buying correctness.

**`REPEATABLE READ` is between the two and gives us nothing extra over `READ_COMMITTED`.** The non-repeatable-read class of bug doesn't apply here — we don't read a row twice in the same transaction expecting the same value. We read once, modify, write.

The transaction timeout (`timeoutString = "${transfer.transaction.timeout-seconds:10}"`) is a redundant ceiling on top of the connection-level `statement_timeout=10s`. Both exist so a misconfiguration of either still bounds the request. Operationally, the Postgres-side timeouts are the ones that fire first in practice, because they count the wait *inside* a single running statement: `lock_timeout` (8s) for a `SELECT FOR UPDATE` blocked on a peer's row lock, `statement_timeout` (10s) for any other long-running statement. `@Transactional`'s timeout only fires at the *next* statement boundary, so on a single statement that blocks indefinitely it has no checkpoint at which to act — see §9.1 for the full ordering.

### 4.4 The deterministic-order proof

A small proof that lex-order locking is deadlock-free for our access pattern:

> Let `T1` and `T2` be two concurrent transfer transactions. Each acquires two locks in the order computed by `lexicographic min, then lexicographic max` on the two wallet IDs.
>
> Suppose `T1` and `T2` share at least one wallet — otherwise no contention. Let `w` be a wallet they both touch.
>
> Case 1: `w` is the lex-min for both. Then both try to acquire `w` first. One succeeds, the other waits. The waiter only proceeds after the holder commits or rolls back, releasing `w`. No deadlock.
>
> Case 2: `w` is the lex-max for both. Symmetric; one acquires the other wallet first, the other waits at the second-acquire.
>
> Case 3: `w` is lex-min for `T1` and lex-max for `T2`. Impossible — lex-min/max are functions of the wallet pair, not the transaction. If `T1` and `T2` share `w`, the *other* wallet differs between them; the lex-min/max ordering for each transaction only ranks against the *other* wallet in *that* transaction. ✓ Re-examined: still impossible to deadlock because at most one of {T1, T2} computes `w` as lex-min, depending on the other ID. The waiter on `w` cannot also be the holder of the lex-max that the holder of `w` is waiting for — because the holder of `w` is waiting on its own lex-max, which the waiter does not hold (the waiter is blocked on `w`, hasn't acquired anything yet on this side).

The intuition is simpler than the case analysis: every transaction acquires locks in the same global total order over wallet IDs. A cycle requires two transactions to acquire locks in opposite orders; lex-order rules that out by construction.

### 4.5 Race walkthroughs

#### Race E — Concurrent debits from the same wallet

```
                 wallet_X.balance = 100
T0: T1 begins (X→A 80)             T2 begins (X→B 80)
T1: T1 SELECT FOR UPDATE X (✓)     T2 SELECT FOR UPDATE X (waits)
T2: T1 reads balance=100, ok       (still waiting)
T3: T1 UPDATE X.balance=20
T4: T1 INSERT ledger entries
T5: T1 commits
T6:                                T2 SELECT FOR UPDATE X (✓ — T1 done)
T7:                                T2 reads balance=20 (committed value)
T8:                                T2 sees 20 < 80, throws InsufficientBalance
T9:                                T2 rolls back, FAILED record written
```

T2 sees the *committed* balance under `READ_COMMITTED` at the moment its SELECT runs. The lock guarantees the read happens after T1's commit, not before. Without the lock, T2 could read the snapshot value (100) from before T1 began.

#### Race F — A→B and B→A simultaneously

```
T0: T1 begins (A→B)                T2 begins (B→A)
T1: lex-min(A,B)=A → T1 acquires A T2 also computes lex-min=A → tries to acquire A
T2: T1 has A, T2 waits
T3: T1 acquires B (no conflict)
T4: T1 commits, releases A and B
T5:                                T2 acquires A (✓)
T6:                                T2 acquires B (✓)
T7:                                T2 commits
```

Both compute `lex-min=A`, both go for A first. T2 waits on A; no second lock is contended. Deadlock-free.

If we had locked in *request* order — `from`-then-`to` — T1 would acquire A then wait for B; T2 would acquire B then wait for A; cycle. Postgres would detect (`deadlock_timeout=1s` by default), abort one with SQLState `40P01`, and the abort would surface as a retryable failure. The lex-order avoids the abort entirely.

### 4.6 Alternatives rejected

**`SERIALIZABLE` isolation.** Discussed in 4.3. Adds spurious retries on workloads that don't actually conflict.

**Optimistic concurrency with a `version` column.** Read wallet, compute new balance, UPDATE WHERE version = old. On conflict, retry the whole flow. Rejected because:

- *Retry storm under contention.* A hot wallet under burst traffic produces cascading retries, each taking the full transaction setup cost. Pessimistic locking has the wait happen inside the database where Postgres can fairly schedule it.
- *Failure semantics are different.* An optimistic conflict is "your read is stale" — internally retryable. A pessimistic wait is "your turn is coming" — bounded by `lock_timeout`. Pessimistic gives a clearer SLA.
- *We already have a retry loop for a different race.* Adding a second retry loop nested inside the first doubles the latency tail.

**Application-level mutex (e.g., `synchronized` on wallet ID).** Rejected because the application is multi-replica; in-process locks don't span instances. A correct implementation would need a distributed mutex, which means another infrastructure dependency (Redis, ZooKeeper, etc.) — see next item.

**Redis-/ZooKeeper-based distributed locks.** Adds a second source of truth for who holds the lock. The Postgres advisory lock and `FOR UPDATE` already satisfy the property; adding another layer is more complexity for no correctness gain, and it adds a failure mode (Redis down, what now?) that doesn't exist with Postgres-only.

**No locking, single-threaded service.** A common "simplest thing" answer. Rejected because it doesn't scale beyond one instance, and one instance with a sequential queue is still vulnerable to crash-recovery issues that locking would handle.

### 4.7 Operational consequences

- **`lock_timeout=8s` bounds the worst case.** A waiter never holds a connection longer than 8s. The Hikari pool can survive bursty contention without draining.
- **`deadlock_timeout` is not in our error story.** Lex-order makes it unreachable in this code path. If the metric `database.deadlocks` ever ticks, that is a *correctness alarm*: somewhere a transaction is locking out of order.
- **Dashboards should track wait time on `SELECT FOR UPDATE`.** Postgres exposes this via `pg_stat_activity.wait_event_type='Lock'`. A sustained increase points at a hot wallet that needs sharding or rate-limiting upstream.
- **The 8s timeout assumes transfers are small and fast.** If someone adds a slow operation inside the transaction — an external HTTP call, a long-running computation — both timeouts must be raised, and operationally the wait-time alerts will trigger more often. Resist that temptation; keep the transaction body tight.

### 4.8 Tests that lock this in

- `ConcurrencyIntegrationTest.shouldPreventOverdraftUnderConcurrency` runs 10 parallel debits of 100,000 cents each against a wallet seeded at 500,000 and asserts exactly 5 succeed, exactly 5 return insufficient balance, and the final balance is exactly 0 (no double-spend, no off-by-one).
- `ConcurrencyIntegrationTest.shouldHandleBidirectionalTransfersWithoutDeadlock` runs 20 simultaneous A→B and B→A pairs and asserts every request completes (timeout would prove deadlock) and both wallet balances stay non-negative — the lex-order property in action.
- `TransferIntegrationTest.shouldFailWithInsufficientBalance` confirms the rollback semantics on a single insufficient-balance request, and `LedgerIntegrationTest.shouldNotCreateLedgerEntriesForFailedTransfer` confirms the FAILED record is the only artefact (zero ledger entries written).
- The lex-order property is also verifiable as a property test: any pair of wallet IDs `(a, b)` with `a != b` produces the same `(lock1, lock2)` regardless of `(from, to)` direction.

---

## 5. Ledger

← Overview: [`DESIGN.md` §5](./DESIGN.md#5-ledger)

The ledger is the audit record. The wallet balance is a maintained projection of it. This separation matters because it tells us where the truth lives — when balance and ledger disagree, the ledger is right and the balance is wrong. The schema is structured to make that disagreement impossible at the row level and recoverable at the table level.

### 5.1 The schema

```sql
CREATE TABLE ledger_entries (
    id              UUID        PRIMARY KEY,
    wallet_id       VARCHAR(64) NOT NULL REFERENCES wallets(id),
    transfer_id     UUID        NOT NULL REFERENCES transfers(id),
    entry_type      VARCHAR(8)  NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    running_balance BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_ledger_per_transfer_wallet UNIQUE (transfer_id, wallet_id)
);
```

Each row is one half of a transfer. A complete transfer is two rows sharing a `transfer_id`. The interesting constraint is `UNIQUE (transfer_id, wallet_id)`: it forbids the schema-level shape "three rows for one transfer," which is the bug a future code change would most likely introduce. A duplicate insert violates the constraint, the transaction rolls back, the bug shows up at first integration test.

The DDL is the source of truth for the "double" in double-entry. Application code that follows the rule is observably correct; application code that violates it crashes loudly. We cannot accidentally write a one-sided ledger; the database refuses.

### 5.2 The atomicity loop

`executeBalancedTransfer` writes everything in this order:

1. INSERT `transfers` row in PROCESSED state
2. UPDATE source wallet (debit)
3. UPDATE destination wallet (credit)
4. INSERT debit `ledger_entries` row
5. INSERT credit `ledger_entries` row

All five inside one transaction (§4). If any one fails, all five roll back. The order is engineered for two properties:

- *Wallet UPDATEs come before ledger INSERTs.* The post-UPDATE balance is what we record on `running_balance`. We use the persisted row returned from `walletRepository.save(...)` rather than the in-memory entity:

  ```java
  source.debit(request.amount());
  Wallet persistedSource = walletRepository.save(source);
  // running_balance taken from persistedSource.getBalance(),
  // not from the in-memory `source`
  ```

  The DB is the single source of truth; if a future refactor reorders things and the in-memory entity drifts, the audit record stays right. This is a small, deliberately conservative choice — paying one extra in-memory copy to avoid a class of audit-corruption bug.

- *`transfers` INSERT comes first.* Both ledger rows have a foreign key to `transfers.id`; the ledger inserts cannot precede the transfer insert without violating FK. Postgres enforces this in either order — but inserting transfer first means the FK check on ledger is a fast positive lookup, not a full row creation under flight.

### 5.3 Immutability via triggers

The trigger on `ledger_entries`:

```sql
CREATE OR REPLACE FUNCTION block_ledger_modifications()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Ledger entries are immutable. UPDATE and DELETE operations are prohibited.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_block_ledger_update
BEFORE UPDATE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION block_ledger_modifications();

CREATE TRIGGER trg_block_ledger_delete
BEFORE DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION block_ledger_modifications();
```

This is the database saying "no" louder than the application could. A repository method that tries to UPDATE a ledger row gets the exception, the transaction rolls back, and a maintainer reading the stack trace gets a clear message ("Ledger entries are immutable"). A direct SQL admin path — `psql` against a production database — gets the same exception.

The defensiveness is worth it. Audit-record corruption is the kind of bug that only shows up at compliance review, weeks or months after the fact. The trigger is permanent; the developer convention is fallible. Pinning the invariant at the database layer means the convention can erode and the system stays correct.

### 5.4 Wallets are append-only

V3 adds a symmetric trigger on `wallets`:

```sql
COMMENT ON TABLE wallets IS 'Append-only: rows are created by seed/admin flows and are NEVER deleted by application code. ...';

CREATE OR REPLACE FUNCTION block_wallet_deletion() ...
CREATE TRIGGER trg_block_wallet_delete BEFORE DELETE ON wallets ...
```

Why: `WalletService.getTransferHistory` short-circuits its existence check — if `transfers` returns rows for `wallet_id`, the wallet must exist (the FK on `transfers.from_wallet_id` / `to_wallet_id` is `NOT NULL` and points at `wallets.id`). The optimization saves a `SELECT existsById` per non-empty history call. But it depends on wallets never being deleted; if a future code path or admin script DELETEs a wallet, the optimization becomes a stale-data leak. The trigger pins the invariant at the only place that can enforce it.

The COMMENT is also load-bearing — it surfaces in `psql`'s `\d+ wallets`, which is what a maintainer would see if they were considering a DELETE. The combination of "documentation that travels with the schema" and "trigger that refuses the action" is the right shape.

### 5.5 The covering indexes

`transfers` has two indexes:

```sql
CREATE INDEX idx_transfers_from_wallet_created
    ON transfers (from_wallet_id, created_at DESC)
    INCLUDE (id, to_wallet_id, amount, status, updated_at);

CREATE INDEX idx_transfers_to_wallet_created
    ON transfers (to_wallet_id, created_at DESC)
    INCLUDE (id, from_wallet_id, amount, status, updated_at);
```

Both are *covering* (`INCLUDE`). `TransferRepository.findByWalletId` UNION-ALLs two index-driven sub-queries — one per index — and orders by `(wallet_id, created_at DESC)`. With the INCLUDE columns in place, Postgres serves the typical query as an index-only scan, no heap fetch per row. As `wallet.transfer-history.max-limit` grows (default 500), the heap-fetch cost would otherwise dominate.

`error_message TEXT` is intentionally excluded from `INCLUDE`. Two reasons:

- TEXT can be TOAST'd. Even with `INCLUDE`, an out-of-line TOAST value forces a heap fetch, defeating the index-only-scan benefit.
- jOOQ's H2-based DDL simulator (used during codegen) doesn't support indexes on CLOB-like types.

The few rows where `error_message` is non-null (FAILED transfers) pay one heap fetch each. Acceptable for a column that is rarely the hot-path read.

The DESC direction matches the dominant query order (`ORDER BY created_at DESC LIMIT n`). It's *not* load-bearing — Postgres scans an ascending B-tree backwards at near-identical cost — but matching the dominant scan direction is a small style preference. Future readers should not cargo-cult DESC into unrelated indexes; pick whichever direction matches the dominant scan.

### 5.6 The "compute the balance from the ledger" alternative

A pure-ledger system with no maintained `wallets.balance` would compute the current balance as:

```sql
SELECT COALESCE(SUM(CASE WHEN entry_type='CREDIT' THEN amount ELSE -amount END), 0)
FROM ledger_entries WHERE wallet_id = ?
```

We don't do that. Reasons:

- *Read latency.* Balance reads (`GET /wallets/{id}/balance`) are a hot path. A single-row indexed read on `wallets` is microseconds; an aggregation over potentially millions of ledger rows scales with history length.
- *Sufficiency-check latency inside the transfer transaction.* Same point, but inside the lock window. Slow sufficiency checks extend the time the wallet row is `FOR UPDATE`-locked, which extends the queue behind it.
- *Eventual disagreement is the failure mode we already prevent.* The `CHECK (balance >= 0)` and the in-transaction wallet UPDATE keep the projection synchronized with the ledger as long as both writes succeed atomically. They are atomic by construction (§5.2). Disagreement requires a bug at a place we've already pinned.

For a system that needs to *audit* the balance, the SUM query above is the gold standard. Run it periodically out of band; if it ever disagrees with `wallets.balance`, the ledger is right.

### 5.7 Reversing a transfer

The triggers refuse `UPDATE ledger_entries SET ...` and `DELETE FROM ledger_entries WHERE ...`. The only way to reverse a transfer is to post a *compensating transfer* — a new transfer in the opposite direction with the same amount, an audit reference back to the original, and the same idempotency guarantees.

This is the right shape for a financial system. A reversal that edits the original would erase audit evidence ("did this transfer ever happen?"); a compensating transfer leaves both the original and the reversal in the ledger, and the running balance reflects the net.

We don't ship a reversal endpoint in this scope (§11), but the schema is ready for it.

### 5.8 Tests that lock this in

- `LedgerIntegrationTest.shouldEnforceLedgerImmutability` directly attempts both an `UPDATE` and a `DELETE` on a posted ledger entry; the trigger raises `RAISE EXCEPTION` and the test asserts both surface as `DataAccessException`.
- `LedgerIntegrationTest.shouldCreateTwoLedgerEntriesPerTransfer` and `shouldCreateMatchingDebitAndCredit` together assert the schema-level `UNIQUE (transfer_id, wallet_id)` and the DEBIT/CREDIT pairing for the source and destination wallets.
- `LedgerIntegrationTest.shouldMaintainBalancedLedger` asserts the net ledger balance (sum of credits − sum of debits) over multiple transfers is exactly 0.
- `LedgerIntegrationTest.shouldNotCreateLedgerEntriesForFailedTransfer` asserts a FAILED transfer leaves zero ledger rows for that transfer id — i.e. the audit row exists for forensics but the projection (balance) is never touched.
- `LedgerIntegrationTest.shouldNotDuplicateLedgerOnReplay` asserts repeated replays of the same idempotency key still leave exactly two ledger rows for the original transfer id.
- `LedgerIntegrationTest.shouldTrackRunningBalancesCorrectly` asserts the `running_balance` recorded on each ledger row matches the wallet balance after the transfer.
- Wallet append-only behaviour (`BEFORE DELETE` trigger on `wallets`) has no dedicated test; it is enforced solely by the V3 migration trigger and is not exercised by the integration suite. Adding one is a known gap.

---

## 6. Retries & error semantics

← Overview: [`DESIGN.md` §6](./DESIGN.md#6-retries--error-semantics)

A transfer succeeds, fails on its merits, or fails *mechanically* — the system tried but couldn't finish for reasons internal to the system. The first two are the easy cases. The third is where a wallet service usually goes wrong: classifying a transient internal failure as a permanent caller error, or vice versa. This section walks through the classification.

### 6.1 The shape of a request

Every `POST /transfers` outcome falls into one of nine buckets:

| Outcome | HTTP | Caller meaning | Retry? |
| --- | --- | --- | --- |
| Fresh success | 201 | Transfer created | No (already done) |
| Idempotent replay (success) | 200 | Cached `201` from prior call | No |
| Idempotent replay (failure) | 200 + cached body | Cached failure response | No |
| Bad request | 400 | Validation, missing header | No (fix the request) |
| Unknown client | 403 | `X-Client-Id` not in `clients` table | No |
| Wallet not found | 404 | `from` or `to` wallet missing | No |
| Hash conflict | 409 | Same key, different payload | No (fix the client) |
| In-progress | 409 | Peer request mid-execution on same key | Maybe — retry the same key after backoff |
| Insufficient balance | 422 | Source wallet < amount | No |
| Transient conflict | 503 + `Retry-After` | Server retry budget exhausted | Yes — same key |
| Database timeout | 504 | DB-level timeout (`lock_timeout` 55P03 or `statement_timeout` 57014) | Yes — same key |

The full table (with subtleties of the in-progress vs hash-conflict 409 distinction) lives in [`docs/RETRY_CONTRACT.md`](./RETRY_CONTRACT.md). This section focuses on the server-side mechanics that produce these classifications.

### 6.2 The retry loop

`TransferService.executeTransfer` wraps the call to `TransferExecutionService.tryExecuteTransfer` in a bounded loop:

```java
for (int attempt = 1; attempt <= maxExecutionAttempts; attempt++) {
    try {
        TransferResult fresh = transferExecutionService.tryExecuteTransfer(...);
        return fresh;
    } catch (IdempotencyConflictException e) {
        TransferResult resolved = resolveConflict(clientId, idempotencyKey, requestHash, attempt);
        if (resolved != null) return resolved;
        if (attempt < maxExecutionAttempts) backoffWithJitter(idempotencyKey);
    } catch (WalletNotFoundException e) {
        failureRecorder.recordWalletNotFoundFailure(...);
        throw e;
    } catch (InsufficientBalanceException e) {
        failureRecorder.recordInsufficientBalanceFailure(...);
        throw e;
    }
}
throw new TransientIdempotencyConflictException(...);  // 503
```

Three exit paths from the loop:

- **Success.** `tryExecuteTransfer` returns; `executeTransfer` returns.
- **Business failure** (insufficient balance, missing wallet). The recorder writes a FAILED row in REQUIRES_NEW, then we re-throw — the controller maps to 422/404 via `GlobalExceptionHandler`.
- **Idempotency conflict.** `resolveConflict` reads the row:
  - *Row is null* → peer rolled back. Sleep and retry.
  - *Row exists, hash matches* → replay (return cached body, exit loop).
  - *Row exists, hash differs* → throw `IdempotencyConflictException` (409, exit loop).
  - *Row exists, status IN_PROGRESS* → throw `IdempotencyKeyInProgressException` (409 in-progress, exit loop). [Defensive arm — see §6.5.]

Falling through all `maxExecutionAttempts` iterations means every retry hit "row is null" — every peer kept rolling back. That's the transient-conflict path: throw `TransientIdempotencyConflictException`, mapped to 503 + `Retry-After`.

### 6.3 The conflict-then-rollback race walkthrough

The race the loop exists to handle:

```
T0: R1 begins, claims key (IN_PROGRESS row inserted)
T1:                                 R2 begins
T2:                                 R2 advisory lock (✗ → 409 In-Progress to R2's caller)
T3: R1 hits InsufficientBalance, rolls back transaction
   (R1's IN_PROGRESS row vanishes with the rollback)
T4: R1's TransferService catch arm calls failureRecorder.recordInsufficientBalanceFailure
    (REQUIRES_NEW; this writes a FAILED row independently)
T5:                                 R2's caller retries (typical client, sees 409, sleeps, retries)
T6:                                 R2 advisory lock (✓ — nothing in flight)
T7:                                 R2 INSERT idempotency
   ─ if R1's REQUIRES_NEW commit happened first: ✗ → IdempotencyConflictException
     → resolveConflict reads, hash matches, FAILED → return cached 422 to R2 ✓
   ─ if R2's INSERT wins: ✓ → R2 executes fresh (also fails — same balance), records its own
     FAILED row. Independent retry, both correct.
```

The interesting case is when R2's INSERT wins. The peer record is gone (R1 rolled back, R1's recorder hasn't committed yet). R2 makes a fresh attempt — which is *correct*: both R1 and R2 are independent attempts, R1 just happens to have failed and recorded its failure asynchronously.

Now consider a different race shape — same key, *no* business failure, just a slow peer:

```
T0: R1 begins, claims key
T1:                                 R2 begins
T2:                                 R2 advisory lock (✗ → 409 In-Progress)
T3: R1 acquires wallet locks (slow — peer transaction holds them)
T4:                                 R2 retries (advisory lock now... still held by R1)
T5:                                 R2 returns 409 again
T6: R1 commits
T7:                                 R2 retries
T8:                                 R2 advisory lock (✓), INSERT (✗ — durable claim from R1)
T9:                                 R2 reads, hash matches, replays → 200 with cached body
```

The 409 In-Progress is a *back-off signal*, not a permanent failure. The retry-contract spec says the client should sleep ~1s and retry; eventually the peer commits (or rolls back) and the retry resolves. The retry budget on the server side does *not* cover the 409 In-Progress case — that is handled by the client's retry. Server-side retries cover only the conflict-then-rollback path where the peer has *finished* but the rollback published an absent row.

### 6.4 Backoff with jitter

```java
private void backoffWithJitter(String idempotencyKey) {
    long sleepMs = baseBackoffMillis;
    if (jitterMillis > 0) {
        sleepMs += ThreadLocalRandom.current().nextLong(jitterMillis);
    }
    try {
        Thread.sleep(sleepMs);
    } catch (InterruptedException ie) {
        Thread.currentThread().interrupt();
        throw new TransientIdempotencyConflictException(
                idempotencyKey, transientConflictRetryAfterSeconds,
                Reason.REQUEST_INTERRUPTED);
    }
}
```

Three things to notice:

- **Jitter is required.** Without it, all servers retrying at lockstep arrive at the database simultaneously and amplify the contention they're trying to escape. `ThreadLocalRandom.nextLong(bound)` requires `bound > 0`, which is why `jitterMillis == 0` skips the random call (see comment in `TransferService`).
- **The last attempt skips the sleep.** A sleep after the *final* attempt only adds latency to the 503; the loop exit doesn't benefit from waiting.
- **Interrupt handling restores the flag and throws.** The InterruptedException path is structurally identical to retry-budget-exhausted (both surface as 503), but the `Reason` enum on the exception (`REQUEST_INTERRUPTED` vs the default `RETRY_BUDGET_EXHAUSTED`) carries the cause to the wire body and the log line. This matters for incident response: a 503 during a deploy reads as "system was hot," misdirecting the oncall, unless the reason field says otherwise.

### 6.5 The defensive IN_PROGRESS arm

`resolveConflict` has a switch arm for `case IN_PROGRESS`:

```java
case IN_PROGRESS:
    throw new IdempotencyKeyInProgressException(key);
```

Comment in the code calls this "defensive — unreachable in current wiring." The reason it's unreachable: `INSERT ... ON CONFLICT DO NOTHING` blocks until the conflicting transaction *finalizes*. By the time we observe the conflict and re-read in `resolveConflict`, the peer has already committed (`COMPLETED` or `FAILED`) or rolled back (row gone). IN_PROGRESS is, in theory, transient — observable only if the peer is mid-transaction at the exact moment of our read.

Why keep the arm? Because a future refactor that splits the claim and the work into separate transactions would silently surface IN_PROGRESS to a peer, and the default arm (`IllegalStateException`) would map to 500 — wrong. The 409 In-Progress response is correct on the wire, consistent with the advisory-lock-fast-path response, and gives a future maintainer a clear "this case is handled" signal.

A comment block in the code spells this out. A future maintainer who deletes the arm should first verify that the new wiring cannot surface IN_PROGRESS to a peer.

### 6.6 The 504 path

Postgres-level timeouts fire under two different SQLState codes: `statement_timeout` raises `57014` (`query_canceled`, class 57 `operator_intervention`), and `lock_timeout` raises `55P03` (`lock_not_available`, class 55 `object_not_in_prerequisite_state`). Spring's `SQLErrorCodeSQLExceptionTranslator` consults `sql-error-codes.xml`, where `cannotAcquireLockCodes=55P03` for PostgreSQL routes the lock-wait timeout to `CannotAcquireLockException`; the statement-timeout path lands as `DataAccessResourceFailureException`. The handler covers both with two complementary strategies:

```java
@ExceptionHandler(CannotAcquireLockException.class)             // 55P03 (typed)
public ResponseEntity<...> handleCannotAcquireLock(...)         { return 504; }

@ExceptionHandler(DataAccessResourceFailureException.class)     // 57014 + fallback
public ResponseEntity<...> handleResourceFailure(... ex) {
    if (isTimeoutOrConnectionFailure(ex)) return 504;           // SQLState walk
    return 500;
}

// SQLState walk on the cause chain
if ("57014".equals(state)            // query_canceled (statement_timeout)
        || "55P03".equals(state)     // lock_not_available (defense-in-depth)
        || state.startsWith("57P")   // operator_intervention
        || state.startsWith("08")) { // connection_exception
    return true;
}
```

`55P03` appears in the SQLState walk too — defense-in-depth for a non-standard wrapping path (custom `SQLExceptionTranslator`, future routing change) where it would arrive as `DataAccessResourceFailureException` instead. The class-55 match is `equals("55P03")`, not `startsWith("55")`, because class 55 also includes non-timeout states (`55006` `object_in_use`, `55000` generic). The string-matching alternative ("`lock timeout`" in the message) was rejected because driver-version-and-locale-sensitive: Postgres JDBC driver upgrades or locale changes could silently flip the response code. SQLState is machine-stable. Both timeouts surface as 504 — the symmetry is intentional. See §9.3 for the full status-code table.

### 6.7 The configuration surface

| Property | Default | Effect |
| --- | --- | --- |
| `transfer.retry.max-attempts` | 3 | Cap before 503 |
| `transfer.retry.base-backoff-millis` | 10 | Base sleep between attempts |
| `transfer.retry.jitter-millis` | 10 | Uniform jitter ceiling |
| `transfer.retry.retry-after-seconds` | 1 | `Retry-After` value on 503 |
| `transfer.transaction.timeout-seconds` | 10 | Per-attempt transaction budget |

The constructor of `TransferService` validates each one. `max-attempts < 1` would short-circuit the loop and surface every conflict as 503; we throw `IllegalArgumentException` at construction. `base-backoff-millis < 0`, `jitter-millis < 0`, `retry-after-seconds < 0` are similarly rejected.

The defaults are tuned for low-millisecond contention recovery. An oncall during a hot-shard incident can raise `max-attempts` via env var without redeploy, converting some 503s into successful retries — at the cost of a wider latency tail. Long-term, that's a signal to fix the contention upstream, not to leave the retry budget elevated.

### 6.8 Alternatives rejected

**Unbounded retries.** A loop without a budget converts a transient incident into a self-DoS: every contended request occupies a connection for the duration of every other contended request's retries. The 503 + `Retry-After` is the ceiling that lets the server shed load.

**No backoff (tight loop).** Without a sleep between attempts, three retries finish in microseconds. Peer transactions never get a chance to commit. Every retry sees the same race, every retry fails, the budget exhausts in less time than it takes a peer to write its first ledger row. The 10ms base + 10ms jitter is small but enough.

**Retry inside the transaction.** A `for` loop wrapped around the inner work, all inside `@Transactional`. Doesn't help — the transaction itself failed and would need to roll back; retrying inside a rolled-back transaction is incoherent. The retry has to happen *outside* the transaction, which is exactly why `TransferService` is non-transactional.

**Retry on `WalletNotFoundException` / `InsufficientBalanceException`.** These are caller-permanent failures. Retrying would re-execute and re-fail (assuming the system is otherwise healthy). The catch arm for each writes the FAILED row and re-throws — no retry.

**Retry on 503 / 504 server-side.** That would be a retry of a retry-budget exhaustion or a database timeout; both indicate genuine server stress. The right place for that retry is the *client*, with backoff (`docs/RETRY_CONTRACT.md` §3). Server-side recursion would amplify the stress.

### 6.9 Tests that lock this in

- `TransferServiceTest.shouldBoundConflictRetries` — pin the retry budget directly: a stubbed `TransferExecutionService` always raises `IdempotencyConflictException` and the repository always returns empty on re-read; the test asserts the loop terminates after exactly `DEFAULT_MAX_EXECUTION_ATTEMPTS` invocations (verified via Mockito) and surfaces `TransientIdempotencyConflictException` (which the controller maps to 503 + `Retry-After`), *not* the regular `IdempotencyConflictException` (which would be a 409 hash mismatch).
- `IdempotencyIntegrationTest.shouldConflictWhenKeyReusedWithDifferentPayload` and `shouldConflictWhenKeyReusedWithDifferentWallets` — claim-then-retry-with-different-payload returns 409, *not* 503; the hash-mismatch arm of `resolveConflict` is exercised end-to-end against a real Postgres.
- `TransactionTimeoutIntegrationTest.shouldTimeoutAndCancelIdempotencyKeyUnderLockContention` — the test thread holds `SELECT ... FOR UPDATE` on `wallet_1` from a separate connection; the request thread blocks on the lock, hits the configured 1s `transfer.transaction.timeout-seconds`, surfaces 504, and the idempotency record is rolled back so the next retry sees no claim. Exercises the framework-timer path (`@Transactional` → `setQueryTimeout`); covers the case where the framework timeout is set tighter than the DB-side `lock_timeout`.
- `LockTimeoutIntegrationTest.lockTimeoutReturns504` — sibling of the above with the override roles inverted: `lock_timeout=200ms`, `statement_timeout=30s`, `transfer.transaction.timeout-seconds=30`. The request thread blocks on a foreign `SELECT ... FOR UPDATE`, Postgres aborts the wait at 200ms with SQLState `55P03`, Spring's vendor codes route to `CannotAcquireLockException`, `handleCannotAcquireLock` maps to 504. Catches a regression that drops the typed handler (would surface as 500). Asserts the duration floor (≥200ms) so a short-circuit "fail-open" change can't pass, and the ceiling (<5s) so the framework timer must not preempt the DB-side bound.
- `GlobalExceptionHandlerTest` (8 unit tests) — locks down the §6.6 SQLState→HTTP mapping in-process. Covers both arms: typed `CannotAcquireLockException` for `55P03`, and the SQLState walk on `DataAccessResourceFailureException` for `57014`/`55P03` (defense-in-depth)/`57P01`/`08006` plus nested cause-chain. Negative tests guard against over-matching: `23505` (`unique_violation`) and `55006` (`object_in_use`, class 55 non-timeout) must surface as 500, not 504. Cheap to run (no Testcontainers); catches a regression on either translation arm before the integration suite would.
- The conflict-then-rollback racing scenarios (peer rolls back mid-retry, retry succeeds on a fresh attempt) are not exercised end-to-end against a live Postgres; the unit-level retry budget bound is the load-bearing assertion. Adding an integration test that wedges the rollback timing deterministically is a known gap.

---

## 7. Failure recording

← Overview: [`DESIGN.md` §7](./DESIGN.md#7-failure-recording)

A successful transfer commits the idempotency claim alongside the work — the row that proves "this key has been used" is durable on the way out. A *failed* transfer is harder. The execution transaction rolls back, which means the IN_PROGRESS claim row vanishes with it. If we do nothing further, the next retry sees no claim and re-executes the whole transfer. For business failures the retry would re-fail (probably) but the contract is stronger than that: the assignment says duplicates must return *the original result*, including the original error. So a FAILED record has to land somewhere, in a transaction that can survive the executing rollback. This section walks through the mechanics, the lost-claim race they create, and the alternatives we ruled out.

### 7.1 The shape of a failure path

Two business-failure exceptions surface from the executing transaction:

- `WalletNotFoundException` — one of the wallet IDs in the request does not exist in the `wallets` table. Surface as 404.
- `InsufficientBalanceException` — both wallets exist; the source wallet has less than `amount`. Surface as 422.

Both bubble up from `TransferExecutionService.tryExecuteTransfer`. They are caught in the *outer* `TransferService.executeTransfer` retry loop, where two things happen in this order:

1. The catch arm calls `IdempotencyFailureRecorder.recordWalletNotFoundFailure(...)` or `recordInsufficientBalanceFailure(...)`. That method is `@Transactional(propagation = Propagation.REQUIRES_NEW)`, which means Spring opens a brand-new transaction independent of any caller-side context. (`TransferService` is non-transactional anyway, but `REQUIRES_NEW` makes the property load-bearing rather than incidental.)
2. The catch arm re-throws. `GlobalExceptionHandler` maps it to the right status code on the wire.

The recorder writes the FAILED row first, the controller writes the response second. From the client's perspective this is invisible — both happen before the response leaves the server. From the *next* retry's perspective it is everything: the cached body the recorder just wrote is what the duplicate will replay.

### 7.2 Why `INSERT`, not `UPDATE`

A symmetric `markFailed` UPDATE on the IN_PROGRESS row was the first design we wrote down and the first one we deleted. The trap:

```
T0: R1 INSERTs IN_PROGRESS claim
T1: R1 hits InsufficientBalance, transaction rolls back
   (R1's IN_PROGRESS row is gone)
T2: R1's catch arm calls markFailed UPDATE in REQUIRES_NEW
T3: UPDATE idempotency_records SET status='FAILED' WHERE (clientId, key) = (...)
   → 0 rows affected (the row was rolled back)
T4: silent no-op. Next retry sees absent row, re-executes the whole transfer.
```

The UPDATE has nothing to update. Postgres reports zero rows affected, the JDBC driver returns 0, application-level code that doesn't read the affected-rows count happily proceeds — and the first sign anything went wrong is that retries don't dedupe.

`INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING` is the right primitive instead. It claims the key from a fresh, blank state — *if* no concurrent retry has already claimed it. This is exactly the same primitive `TransferExecutionService` uses on the happy path; the symmetry is intentional.

`IdempotencyRecordRepository` deliberately exposes no `markFailed` method. The only failure-recording entry point is `insertFailedRecord`, which compiles to `INSERT ... ON CONFLICT DO NOTHING` and returns the affected-row count to the caller. A future maintainer who wants to "just UPDATE the row" has to add a method to the repository, which is a code review prompt to ask why.

### 7.3 The wallet-not-found vs insufficient-balance split

Two recorder methods, two different shapes:

**`recordWalletNotFoundFailure`.** No `Transfer` row is written. The reason is structural — `transfers.from_wallet_id` and `transfers.to_wallet_id` are FOREIGN KEY columns into `wallets`. If the request named a nonexistent wallet, the FK cannot be satisfied. Writing a Transfer row would either crash with a `ForeignKeyViolation` (`23503`) or require relaxing the FK (which would break audit invariants forever for a 404). We accept "no Transfer audit for 404" as the correct trade — the `idempotency_records` row alone, with status 404 and the cached error body, is enough to dedupe replays.

**`recordInsufficientBalanceFailure`.** Both wallets exist (we know because we got past the `findByIdForUpdate` step before the balance check fired). So we *can* write a FAILED Transfer row, and we do — explicitly for audit. The order is:

```java
Transfer transfer = Transfer.failed(from, to, amount, errorMsg);
transferRepository.insert(transfer);          // (1) audit row first

int claimed = idempotencyRecordRepository.insertFailedRecord(
        clientId, idempotencyKey, requestHash, transfer.getId(),
        HttpStatus.UNPROCESSABLE_ENTITY.value(), errorJson);  // (2) claim with FK
if (claimed == 0) {
    log.info("Idempotency claim lost to concurrent retry — keeping audit Transfer row");
}
```

The order matters. `transfers` has no FK back to `idempotency_records`, so an inserted Transfer row that loses the subsequent idempotency claim is *legal* (it's just unreferenced). The inverse — an idempotency record claiming a Transfer that was never inserted — would surface as a FK violation and the whole REQUIRES_NEW transaction would fail. Audit-row-first means the FK check on step (2) is a fast positive lookup against the row we just wrote.

### 7.4 The lost-claim race

The two recorder calls run inside REQUIRES_NEW, but a peer retry can still beat them to the punch:

```
T0: R1 IN_PROGRESS, business failure, rolls back
T1: R2 IN_PROGRESS (advisory lock free, claim row free)
T2: R1's REQUIRES_NEW recorder begins
T3: R2 finishes, commits a COMPLETED record (or a FAILED of its own)
T4: R1's recorder INSERT (with ON CONFLICT DO NOTHING) returns 0
T5: R1's catch arm sees claimed == 0; logs "claim lost to concurrent retry"
T6: R1 client gets 422 with the original error body
T7: any *later* duplicate gets R2's COMPLETED body
```

Two response shapes for one logical key, depending on which retry the client landed on. Is that a contract violation? No — the assignment says duplicates of the same key must return the *original* result, where "original" means the result of whatever attempt actually succeeded in claiming the key. R2's claim won; R2's response is the canonical one. R1's response is ephemeral — it goes to one client over one TCP connection and then nothing; later replays don't see it.

What R1 *does* leave behind, in the insufficient-balance branch, is an unreferenced FAILED Transfer row — written at step (1) before the lost claim at step (2). This is the "orphan FAILED transfer" the comment in `recordInsufficientBalanceFailure` documents. The orphan is bounded — at most one per failed-then-lost-claim race per failure — and the operational contract is explicit:

> reconciliation tooling that joins `transfers` to `idempotency_records.transfer_id` MUST tolerate orphaned FAILED transfers (LEFT JOIN, not INNER JOIN).

We accept the orphan because the inverse — a cached 422 with no audit row — would be much worse. A 422 means the system *says* the transfer was attempted and failed; an absent audit row would make that claim unverifiable. The orphan preserves auditability at the cost of a small reconciliation footnote.

### 7.5 Why `afterCommit` for the metric

`recordFailureMetricAfterCommit` registers the metric increment on the `afterCommit` hook of the active REQUIRES_NEW synchronization rather than incrementing immediately:

```java
if (TransactionSynchronizationManager.isSynchronizationActive()) {
    TransactionSynchronizationManager.registerSynchronization(new TransactionSynchronization() {
        @Override public void afterCommit() {
            metrics.recordFailure(reason);
        }
    });
} else {
    metrics.recordFailure(reason);
}
```

The hazard the hook closes: the recorder INSERT can *succeed at the application level* (Postgres returns 1 row affected) but the surrounding transaction can still fail at commit — disk write error, replication lag check, network blip, statement timeout firing on the COMMIT itself. If we incremented the counter at the moment the JDBC `executeUpdate` returned, those edge cases would leave the counter ahead of the durably persisted state. In a high-volume system that produces a slow drift between "what the dashboard says happened" and "what actually persisted," and the drift is impossible to reconcile after the fact.

`afterCommit` is fired by Spring only after the underlying JDBC commit returns successfully. If the commit fails, the `afterRollback` arm fires instead and the counter is not bumped. Counters are tied to durable state, not to in-flight attempts.

The fallback branch (`metrics.recordFailure(reason)` directly) handles the case where this code is called outside any active transaction synchronization — defensively, in case some future test wiring or cleanup path bypasses the REQUIRES_NEW boundary. In normal production both methods on the recorder are `@Transactional(REQUIRES_NEW)` and the synchronization is always active.

### 7.6 The captured-time question

The error JSON cached in `response_body` includes a `timestamp` field set to `Instant.now()` at the moment of capture. Replays return this stored timestamp, not the replay-time:

```java
private String formatErrorJson(HttpStatus status, String message) {
    Map<String, Object> body = new LinkedHashMap<>();
    body.put("timestamp", Instant.now().toString());
    body.put("statusCode", status.value());
    body.put("error", status.getReasonPhrase());
    body.put("message", message);
    return objectMapper.writeValueAsString(body);
}
```

Two reasons we chose capture-time over replay-time:

- *Audit fidelity.* The response always reflects when the failure occurred, not when the client retried looking at it. A client that reads the timestamp and correlates with their logs gets a stable anchor.
- *Byte-equality on replay.* The cached body is shipped verbatim through the `TransferResult.Replay.rawJson` path (§3); regenerating the timestamp on each replay would force re-serialization through Jackson and break the verbatim guarantee.

The integration test `IdempotencyIntegrationTest.shouldReturnIdenticalErrorResponseOnReplay` asserts the bodies are byte-equal across replays. Replay-time `timestamp` would have failed that test by construction, which is the test's job: the design choice is in the assertion.

### 7.7 Failure mode without the `REQUIRES_NEW` boundary

A useful counterfactual: what would break if `IdempotencyFailureRecorder` were `@Transactional(REQUIRED)` (the default) instead of `REQUIRES_NEW`?

REQUIRED joins the caller's transaction. In our wiring, `TransferService.executeTransfer` is not `@Transactional`, so REQUIRED would open a fresh transaction *for the recorder's first call*. That sounds equivalent to REQUIRES_NEW — and on the happy path it is.

The break shows up if a future refactor makes `TransferService.executeTransfer` transactional, or if some test wiring inadvertently nests the recorder inside a transactional caller. With REQUIRED, the recorder would join the outer transaction. The outer transaction is the one that *just rolled back* at the executing-service boundary — and even though Spring's transaction manager has marked the outer as rolled back by the time we re-throw, joining a rolled-back transaction's continuation can subtly change semantics depending on the propagation chain. REQUIRES_NEW makes the recorder's lifecycle independent by construction. The choice survives refactors that REQUIRED would silently break.

### 7.8 Alternatives rejected

**`markFailed` UPDATE.** Already covered (§7.2). The row to UPDATE doesn't exist; the operation no-ops; the bug is silent.

**Write the FAILED row inside the executing transaction, before rolling back.** Pre-INSERT a FAILED record, then do the work, then UPDATE on success. Rejected: success path now does two writes to the idempotency record (INSERT + UPDATE) instead of one (INSERT with `markCompleted` UPDATE folded in), and the FAILED row written at the top of the transaction would itself roll back if the transaction fails for *any* reason — including network blips and statement timeouts — leaving us back at the original problem with extra writes on the way.

**Use `SAVEPOINT` to keep the idempotency claim while rolling back the rest.** `INSERT idempotency`, `SAVEPOINT s`, do the work, on failure `ROLLBACK TO SAVEPOINT s`, mark the claim FAILED, commit. This works in principle. Rejected because Spring's nested-transaction support requires `JpaTransactionManager` or specific JDBC machinery, and the savepoint protocol leaks complexity into every code path that touches the idempotency table. REQUIRES_NEW achieves the same outcome with one annotation and zero protocol changes. The savepoint approach is also harder to reason about — a future contributor reading the code has to think about which writes are inside the savepoint and which are outside.

**Async event recording.** Emit a "transfer failed" event to a queue; a worker writes the FAILED record. Rejected because it inverts the dedup contract — a duplicate request that arrives between the failure and the worker's write would re-execute the transfer, with all the side effects that implies. Synchronous recording inside REQUIRES_NEW closes that window.

**One recorder method that handles both 404 and 422.** Rejected for two reasons. First, the FK semantics differ — the 404 path *cannot* write a Transfer row, the 422 path can. A unified method would need a conditional that's hard to read. Second, the call sites differ — the 404 path doesn't know `availableBalance` (no wallet was found, no balance to report), while the 422 path needs it for the error message. Two methods, each one's signature self-describing, beats one method with optional arguments.

### 7.9 Operational consequences

- **A non-zero `idempotency.failure.lost_claim` rate is normal under contention.** Each one indicates a genuine concurrent retry — the system handled the race correctly. Watch for sudden spikes (a misbehaving client retrying without backoff) but the steady-state rate is not actionable.
- **Orphan FAILED transfers grow with the lost-claim rate.** The growth is bounded — at most one orphan per lost-claim race per failure. The current design accepts the orphans (LEFT JOIN tolerance in reconciliation tooling). A periodic sweep that flags FAILED transfers without an inbound `idempotency_records.transfer_id` reference older than the replay TTL is documented in the recorder comment as a backlog item, not a silent dependency.
- **The cached error body is captured once.** Operators should not be surprised when the timestamp on a replay disagrees with the wall clock by minutes or hours — that's the capture-time being faithful to the original event.
- **Per-reason failure metrics only move on commit.** A counter that doesn't budge during a brownout is correct — failures that didn't durably persist shouldn't show up.

### 7.10 Tests that lock this in

- `IdempotencyIntegrationTest.shouldReplayFailedTransferCorrectly` — first request fails with 422, second request with the same key returns the same 422 + body (not a re-evaluation that might succeed if balance changed).
- `IdempotencyIntegrationTest.shouldReturnIdenticalErrorResponseOnReplay` — the replay body is byte-equal to the original; captures the captured-time semantics.
- `LedgerIntegrationTest.shouldNotCreateLedgerEntriesForFailedTransfer` — confirms the FAILED Transfer audit row exists for an insufficient-balance failure but has zero ledger entries; pins the audit-without-ledger shape that the recorder produces.
- `TransferIntegrationTest.shouldFailWhenSourceWalletNotFound` / `shouldFailWhenDestWalletNotFound` — surface the 404 path; combined with the replay test above, proves the recorder's wallet-not-found arm caches the 404 body.
- `TransferIntegrationTest.shouldFailWithInsufficientBalance` — surface the 422 path.

The lost-claim race is genuinely hard to test deterministically (it requires precise interleaving of two REQUIRES_NEW transactions on the same key). The integration suite covers the *outcomes* the race produces — same key, same response body across replays — without trying to script the interleaving directly. Property-style tests (run N concurrent retries, assert exactly one canonical response and at most one orphan transfer) are a backlog item.

---

## 8. Authentication

← Overview: [`DESIGN.md` §8](./DESIGN.md#8-authentication)

The service ships with a two-layer story for *who is sending this request*. `X-Client-Id` carries the caller's identity; HMAC, when enabled, carries the *proof*. The two are deliberately separable: the idempotency machinery cares about the identity (so it can scope keys per-tenant) but does not require the proof; the proof, when present, is what stops a bad actor from forging that identity. This section walks through both layers, the sign-and-verify protocol, the hazards, and the alternatives we ruled out.

### 8.1 Two roles for `X-Client-Id`

`X-Client-Id` is doing two jobs at once, and conflating them is the easiest way to misread the design.

**Role 1: namespace on the idempotency key.** The PRIMARY KEY of `idempotency_records` is `(client_id, idempotency_key)`. Two callers using the same `Idempotency-Key: abc123` are two independent claims — one row per caller. Without this scoping, key collisions across tenants would silently alias unrelated requests; caller A's `abc123` would replay into caller B's response. The integration test `TransferIntegrationTest.shouldAllowSameIdempotencyKeyForDifferentClients` pins this: same key, two clients, two distinct transfers, two distinct response bodies.

**Role 2: lookup key for the shared HMAC secret.** With signing enabled, `X-Client-Id` is what `SignatureFilter` reads to know which secret to verify against. *Today the same global secret verifies every client* — a known limitation with a concrete cross-tenant hazard, walked through in [§8.11](#811-known-limitation-single-shared-signing-secret-across-clients). The fix (a per-client config map plus a resolver) is small and sketched there; deferring it was an assignment-timebox call, not a design preference.

The role separation matters because *only role 2 is authenticated*. With HMAC off, the filter chain accepts any `X-Client-Id` value and the namespace is unauthenticated — caller A can send `X-Client-Id: caller-b` and write into caller-b's idempotency namespace. With HMAC on, signing the request requires possessing caller-b's secret; the namespace is authenticated by construction.

The DESIGN.md elevator pitch puts this bluntly: *with HMAC disabled, `X-Client-Id` is unauthenticated and trivially forgeable.* The deep-dive view: that's by design for the assignment's threat model (network is trusted, caller is trusted), and HMAC is the off-the-shelf flip-the-switch upgrade.

### 8.2 Off-by-default with a loud startup line

The constructor of `SignatureFilter` runs exactly once at boot and emits one of two log lines:

```
INFO  - security.signature.enabled=true (HMAC required on state-mutating endpoints)
WARN  - security.signature.enabled=false. DO NOT run this configuration in production.
```

Two design choices baked in:

- **Off by default.** The assignment has to be exercisable without provisioning a secret — graders, CI runners, and curl-from-the-prompt all need to work. So `security.signature.enabled=false` is the default in `application.yml`, and the integration tests run with HMAC *on* via `application-test.yml` (the test secret is checked in; production deployments override it via env var).
- **WARN-level when off, with a grep-friendly marker.** A production deploy that ships the off configuration is the most dangerous misconfiguration this service has — every state-mutating endpoint becomes unauthenticated. So the off path logs at WARN, with the literal string `DO NOT run this configuration in production` that an alerting pipeline can match on. The on path logs at INFO so ops can confirm the secure mode is active without it triggering a noise alert.

A second guard at construction: `signatureEnabled=true` *and* an empty/blank secret throws `IllegalStateException` at startup. HMAC against an empty key is trivially forgeable (the secret is the entire authentication primitive), so the application refuses to start. That fail-fast beats the alternative — boot up with a useless secret and hope nobody runs `nc localhost 8080` — by a wide margin.

### 8.3 The signed surface

The string the HMAC is computed over:

```
method:path:clientId:idempotencyKey:timestamp:sha256(body)
```

Six components, each load-bearing:

- **`method`** (uppercased). `POST` and `PUT` to the same path with the same body are different requests; signing the method prevents a `POST` signature from being replayed as a `PUT` (or vice-versa) against an endpoint that supports both.
- **`path`** (raw `request.getRequestURI()`, not the normalized form). Signing the path binds the signature to the exact route. If a future routing change adds a new endpoint, signatures bound to the old path don't accidentally authorize the new one.
- **`clientId`**. Binds the signature to the caller. A signature minted by client A cannot be replayed under `X-Client-Id: client-b` because the HMAC computation includes the client identifier — the verifier recomputes with the *received* `X-Client-Id` and the secret-for-that-client; mismatched IDs produce a mismatched signature.
- **`idempotencyKey`**. Binds the signature to a specific logical request. An attacker who captured a valid signature cannot replay it under a different `Idempotency-Key` because the recomputation would not match.
- **`timestamp`** (epoch millis as a string). Bounds the signature's validity window. Without it, a captured signature is forever-valid; with it, the signature is valid for the configured TTL (default 5 minutes).
- **`sha256(body)`**. Binds the signature to the *contents* of the request, not just its existence. A man-in-the-middle that mutates `amount` from `100` to `100000` invalidates the signature even though every other surface header is unchanged.

We sign the SHA-256 hash of the body rather than the raw body bytes for two reasons. First, it bounds the size of the data fed to `Mac.doFinal` — the body could be hundreds of bytes today and conceivably larger tomorrow, while the hash is always 64 hex characters. Second, it makes the signed string printable, which simplifies debugging — an operator can paste the signed string into a script and recompute by hand without worrying about binary-safe escaping.

A subtle property: every component is colon-separated without escaping. If any component were allowed to contain a literal colon, the signed string would be ambiguous — `a:b:c` could parse as `("a", "b", "c")` or `("a:b", "c", ...)` depending on which side splits it. The components in our signed string are constrained against this:

- `method` is one of a small fixed set (`GET`/`POST`/`PUT`/etc.), no colons.
- `path` is a URI, where `:` is reserved for the scheme and not legal in the path component per RFC 3986.
- `clientId` and `idempotencyKey` are rejected upstream if blank, but otherwise we don't enforce a colon-free invariant. *In practice they don't contain colons*, but a future contract that allowed colon-bearing client IDs would need to revisit this. (A small regex check at the filter boundary, or switching to a length-prefixed encoding, are the obvious upgrades.)
- `timestamp` is an integer; no colons.
- `sha256(body)` is hex; no colons.

### 8.4 Constant-time comparison

The verifier compares the received signature to the recomputed expected signature with `MessageDigest.isEqual`:

```java
private boolean constantTimeEquals(String a, String b) {
    if (a == null || b == null) {
        return false;
    }
    return MessageDigest.isEqual(a.getBytes(StandardCharsets.UTF_8), b.getBytes(StandardCharsets.UTF_8));
}
```

`MessageDigest.isEqual` runs in time proportional to the *length* of its inputs, not in time proportional to the number of leading bytes that match. The hazard it closes is the timing-side-channel attack: a naive `String.equals` (or `Arrays.equals`) returns false at the first differing byte, leaking which prefix is correct. An attacker who can measure server response time precisely could brute-force a signature one byte at a time — first byte from 256 candidates, then second byte from 256, and so on — with a wall-clock attack that scales linearly in signature length rather than exponentially.

The attack is theoretical against this codebase (the network noise floor swamps the timing signal at our scale), but the cost of using `MessageDigest.isEqual` is zero — it's one method call, the constant-time guarantee is an existing property of the JDK — and the cost of *not* using it is a permanent latent vulnerability against future deployments where the timing signal is observable.

### 8.5 Body caching: filter coordination

The body is read twice in a normal request: once in `SignatureFilter` (for the HMAC computation), and again in the controller (for Jackson JSON binding). `HttpServletRequest.getInputStream()` is single-pass — once the bytes have been read, they're gone. Without coordination, the second reader would see an empty stream and the controller would surface a confusing "request body is missing" error.

`CachedBodyHttpServletRequest` is the coordination point. It wraps the inbound request, copies the body into a `byte[]` at construction, and overrides `getInputStream()` and `getReader()` to return fresh streams over the cached bytes on every call:

```java
public CachedBodyHttpServletRequest(HttpServletRequest request) throws IOException {
    super(request);
    this.cachedBody = StreamUtils.copyToByteArray(request.getInputStream());
}

@Override
public ServletInputStream getInputStream() {
    return new CachedBodyServletInputStream(this.cachedBody);
}
```

`IdempotencyHeaderFilter` (Order +10) wraps the request and forwards the wrapped instance down the chain. `SignatureFilter` (Order +20) sees the wrapper, reads the cached bytes for HMAC, and forwards. The controller (Spring MVC, after the filter chain) sees the wrapper too and Jackson reads the same cached bytes for binding. One copy, three readers.

The runtime assertion in `SignatureFilter`:

```java
if (!(request instanceof CachedBodyHttpServletRequest cbr)) {
    throw new IllegalStateException(
            "SignatureFilter requires a CachedBodyHttpServletRequest "
                    + "(IdempotencyHeaderFilter must run earlier). "
                    + "Check filter @Order configuration.");
}
```

This is a fail-fast guard against filter-order misconfiguration. If a future change moves the order numbers around or adds a new filter that bypasses the wrapping, the very next request to `/transfers` throws `IllegalStateException` and the filter chain returns 500. That's loud, immediate, and impossible to misread — much better than the silent failure mode where `getInputStream()` returns an empty stream and the controller responds with a "bad JSON" 400, which would mislead anyone debugging the chain. The comment in the code spells out why we don't fall back to a "just read the bytes here" arm: doing so consumes the body in the filter, which strictly worsens the failure mode rather than improving it.

### 8.6 Timestamp validation

Two checks bound the timestamp:

```java
if (now - timestamp > ttlMillis) {
    // expired (older than 5 min default)
    return 401;
}
if (timestamp - now > CLOCK_SKEW_TOLERANCE_MILLIS) {
    // future-dated beyond skew (5 min)
    return 401;
}
```

The first check is the replay-window bound — a captured signature stops being valid 5 minutes after the timestamp it was minted with. The second check is symmetric for client clocks running ahead — we accept up to 5 minutes of forward skew to absorb client-clock drift, but a client whose clock is hours ahead of ours would otherwise indefinitely-forward-date its requests. The two-sided check is a small detail that closes a class of attack: without the future-dated bound, an attacker could mint a signature timestamped 24 hours from now and replay it indefinitely from now until 24h - 5min from now (i.e., the entire 24-hour future window).

The 5-minute TTL is configurable (`security.signature.ttl-minutes`, default 5). The integration tests run with a wider TTL (30 min) because shared CI workers can have clocks that drift by minutes — the test config (`application-test.yml`) explicitly comments on this: *"30 minutes is enough for normal runs but leaves no margin for slow startup or paused containers."* Production should not use the test TTL; the comment is intentionally narrow about the trade-off.

The future-dated tolerance is *not* configurable (it's a hard-coded 5-minute constant). That's deliberate — any future-dated tolerance is a window for replay, and the case for tuning it on a per-deployment basis is weak. Five minutes is what we accept; a deployment that needs more is one that has a clock hygiene problem to fix instead.

### 8.7 What HMAC is *not*

The HMAC layer protects against forged identity and tampered payloads. It does not protect against:

- **Replay within the TTL window.** A captured signature is valid for 5 minutes; an attacker who captures it can re-issue the same request bytes and the verifier accepts it. Replays of *successful* state changes are absorbed by idempotency (same `(clientId, idempotencyKey)` → cached response). Replays of *failures* are similarly absorbed (cached error body). Replays under a *different* idempotency key are not absorbed — they would forge a fresh successful transfer using the captured signature, but only if the attacker can also forge the idempotency key, which loops back to the signature: a fresh `Idempotency-Key` paired with the captured signature won't verify because the HMAC includes the key. So the replay attack against this design requires capture-and-replay of the *exact* request bytes within 5 minutes, and the result is at most a duplicate request that the idempotency machinery dedupes. Still, a deployment with stricter requirements should layer transport security (TLS, mTLS) on top of HMAC.
- **Transport-level confidentiality.** The HMAC is over the request shape, not the request privacy. A wire observer who captures the request sees the body. TLS is the right tool for that; HMAC is orthogonal.
- **Per-wallet authorization.** "Caller A is allowed to debit wallet X but not wallet Y" is not modeled (§11). HMAC verifies that the caller is *who they claim to be*, not that the claimed caller is allowed to do the requested thing.

Reading these as gaps in the *authentication* layer is wrong — they are intentional scope boundaries. The HMAC layer answers one question (is this `X-Client-Id` authentic?) and the answer is composable with whatever sits in front (TLS, gateway-level authorization).

### 8.8 Alternatives rejected

**JWT bearer tokens.** A JWT would authenticate the caller without the HMAC ceremony — sign once, present the token on every request, the verifier checks the signature. Rejected because JWTs are *signature over a token*, not *signature over the request*. A captured JWT is replayable against any payload; it doesn't bind to the request body or the idempotency key. To get the same protection as our HMAC scheme, the JWT-based design would still need a body-hash field somewhere — likely a custom header — at which point the JWT is doing extra work for no extra protection.

**API keys without signing.** Send a static `Authorization: Bearer <key>` and call it a day. Rejected for the same body-tampering reason: a captured key is reusable against any payload. The simplest version of this design ("we'll just check the key matches") is the exact threat model HMAC is designed to defeat.

**Mutual TLS as the only authentication.** mTLS authenticates the connection but doesn't authenticate the request body — a compromised client cert can issue any payload it likes against the connection it owns. mTLS is *complementary* to HMAC, not a substitute, and we left it out of scope to keep the deployment surface small.

**Database-backed nonce table for replay protection.** Persist every seen `(clientId, signature)` for the TTL window; reject any signature seen before. Rejected because idempotency already provides the replay-defence we care about (same key → cached response), and a separate nonce table would double the write rate for no incremental defence. The existing 5-minute TTL on the timestamp gives us a bounded replay window, and idempotency closes the rest.

**Per-request key rotation.** Each request derives a new key from a master via HKDF and signs with the derived key. Rejected as overengineering for the threat model — the master key already needs to be protected; deriving doesn't change that.

### 8.9 Operational consequences

- **A `WARN` line at startup is the canary.** The first thing an oncall should check during a "why are unauthenticated requests reaching the controller?" incident is whether `security.signature.enabled` is `false` in the loaded config.
- **The shared secret is sensitive.** It must be sourced from a secret manager, not committed to source control. The test secret in `application-test.yml` is committed deliberately because the test profile is never loaded in production; conflating the two profiles is the deployment hazard.
- **Secret rotation is manual today.** Rotating the secret requires updating the deployment config and restarting. A graceful rotation (overlap window where both the old and new keys verify) is a backlog item — for now, signed clients should retry on `401 Invalid X-Signature` after the rotation completes.
- **Clock drift is an operational concern.** A client whose clock drifts beyond the 5-minute future tolerance starts seeing `401 X-Timestamp is in the future`. Server-side NTP sync is the corresponding obligation; without it, a server clock that drifts behind real time would reject otherwise-valid requests.
- **Filter ordering is fragile but checked.** A future contributor who reorders filters and breaks the wrap chain triggers the runtime assertion on the very next request. The assertion is the regression test.

### 8.10 Tests that lock this in

- `TransferIntegrationTest.shouldAllowSameIdempotencyKeyForDifferentClients` — namespace property: same key under two clients yields two transfers.
- `TransferIntegrationTest.shouldReject403ForUnknownClient` — `X-Client-Id` references a client not in the registry; service rejects with 403 even though HMAC is valid (the HMAC is over the supplied unknown ID, so it verifies; the service-layer client lookup is what catches it). Demonstrates that HMAC verifies authenticity, not authorization.
- The integration suite runs with `security.signature.enabled=true` (`application-test.yml`); every test that hits a mutating endpoint produces and verifies a real HMAC. A regression in the signature pipeline fails the entire suite, not one test.
- Unit tests for `SignatureFilter` cover: missing `X-Client-Id` → 400, missing/invalid `X-Timestamp` → 400, expired timestamp → 401, future-dated timestamp beyond skew → 401, missing `X-Signature` → 400, mismatched signature → 401.
- The `IllegalStateException` from the cached-body assertion is exercised by misconfiguring filter order in a unit test that wraps the filter without the upstream wrap — surfaces as an explicit failure, not a silent bypass.
- Constructor-validation: `signatureEnabled=true` with empty/blank secret fails with `IllegalStateException` at boot; covered by a Spring context-loading test that asserts the application refuses to start.

### 8.11 Known limitation: single shared signing secret across clients

The current wiring uses **one global HMAC secret** for every client. `SignatureFilter` reads `security.signature.secret` once at startup and verifies every incoming HMAC against it, regardless of which `X-Client-Id` the request carries. That collapses the per-client identity from §8.1 into a single shared key, and it has a concrete failure mode worth naming.

**The hazard.** Any party that holds the global secret can mint a valid signature under *any* `X-Client-Id`. A compromised client A — leaked log, stolen config, malicious insider on the client side — can issue requests that claim to be from client B by sending `X-Client-Id: client-b` and signing with A's copy of the shared secret. The HMAC verifies (the secret matches), the filter passes, and the request lands in B's idempotency namespace as if B sent it. The blast radius is bounded by what the wallet contract allows the *claimed* client to do (today: nothing client-specific — wallets are not owned; see §11.1), but the *attribution* is wrong: B's logs and B's idempotency rows show traffic that B never originated. For a multi-tenant deployment where attribution drives billing, audit trails, or per-tenant rate limits, that is a real cross-tenant forgery — not a theoretical one.

**Why we shipped this shape.** The assignment's threat model does not require per-client secrets, and the existing wiring documents the gap rather than hiding it: the `clients` table deliberately has no `signature_secret` column (HMAC keys at rest in plaintext are a `pg_dump`-leak-class antipattern), and §8.1 already calls the per-client lookup *a small step away* from the current shape. Shipping with the global secret kept the deployment surface small for the assignment timebox; the cost is the cross-tenant property above.

**The fix, concretely.** Replace the flat `security.signature.secret` with a map binding:

```yaml
security:
  signature:
    enabled: true
    secrets:
      client-a: "<32+ random bytes for A>"
      client-b: "<32+ random bytes for B>"
    ttl-minutes: 5
```

Introduce a `ClientSignatureSecretResolver` interface (`Optional<String> resolve(String clientId)`), back it with a `@ConfigurationProperties("security.signature")` bean, inject the resolver into `SignatureFilter`, and look up the secret per request. An unknown `X-Client-Id` (no entry in the map) returns 401 — the filter never falls through to a shared key, so the failure mode is *deny* rather than *accept-with-wrong-attribution*. Production secret material lives in a vault, sourced via env vars (`SECURITY_SIGNATURE_SECRETS_<CLIENTID>`); the test profile keeps a checked-in map keyed by the integration-test client ids.

The wiring is genuinely small (one interface, one config-properties record, ~10 lines of filter changes), but the test surface that proves the property grows by one decisive case: a `SignatureValidationTest.shouldRejectCrossClientSignature` test signs as A, claims to be B, and asserts 401 — the regression test for what the global-secret design cannot enforce.

**What partially mitigates the risk today.** Three other layers reduce — but do not eliminate — the practical impact of the shared-secret design:

- **No per-wallet authorization.** Wallets are not owned by clients (§11.1), so the cross-tenant forgery cannot be used to debit wallets that the victim *exclusively* controls — there is no such notion. The forgery is a *naming* attack (B's idempotency row, B's logs) more than a *value* attack.
- **The idempotency namespace is per-client.** A forged request under B populates B's `idempotency_records` row, but it cannot replay a *legitimate* request B sent earlier under a different key — the keys are independent.
- **The HMAC layer still verifies authenticity of the request bytes.** A wire-level attacker who never holds the global secret cannot tamper with the body; the cross-tenant hazard is specifically the *insider-with-the-secret* scenario, not the *random-network-attacker* scenario.

These three properties make the global-secret design acceptable for the assignment's threat model. They do not make it acceptable for a real multi-tenant deployment, and a hardening review before opening the service to production traffic must close this gap.

---

## 9. Timeouts & observability

← Overview: [`DESIGN.md` §9](./DESIGN.md#9-timeouts--observability)

Two unrelated subsystems share this section because they're load-bearing in the same way: both turn questions an oncall has at 3am ("is this request stuck?", "why are we returning 504?") into answers that are *inside the system*, not inferred from external behaviour. Timeouts make stuckness fail fast and visibly; observability makes the failure legible. This section walks through the hierarchy of timeouts and the three layers of observability the service exposes.

### 9.1 The timeout hierarchy

Five timeouts bound a request, each at a different layer:

| Timeout | Source | Default | Fires when |
| --- | --- | --- | --- |
| `lock_timeout` | Postgres, set via `connection-init-sql` | 8s | A statement waits on a row lock longer than this |
| `statement_timeout` | Postgres, same | 10s | Any single statement runs longer than this |
| `@Transactional(timeoutString)` | Spring | 10s | Checked at the *next* statement boundary inside the transaction |
| Hikari `connection-timeout` | Hikari | 10s | A request waits for a connection from the pool longer than this |
| Read/connect timeouts upstream | Load balancer / client | varies | Outside this service's control |

The interesting question is *which one fires first under each failure mode.* The answer dictates the response code, the operator's mental model, and what to put on the dashboard.

**Stuck on a row lock.** A `SELECT ... FOR UPDATE` waiting on a peer transaction trips `lock_timeout` first (8s). Postgres aborts the statement with SQLState `55P03` (`lock_not_available`); Spring's PostgreSQL vendor codes (`sql-error-codes.xml`: `cannotAcquireLockCodes=55P03`) translate this to `CannotAcquireLockException`; `GlobalExceptionHandler.handleCannotAcquireLock` maps it to 504. This is the path you most often see in incidents — a hot wallet under burst contention pulls the queue length up, tail requests time out, and the dashboard shows 504s.

**Long-running query, no lock contention.** A statement that's actually running (not waiting) for >10s trips `statement_timeout`. SQLState `57014` (`query_canceled`); same handler, same 504. This shouldn't happen under normal load — our queries are indexed, the writes are bounded — but a misindexed migration or a `SUM` over the ledger from an admin script could trip it.

**Spring `@Transactional` timeout.** The redundant ceiling. `timeoutString = "${transfer.transaction.timeout-seconds:10}"` on `TransferExecutionService.tryExecuteTransfer` checks the elapsed time *between statements*. If a statement is still running, this timeout doesn't help — Postgres' bound is the active one. But if a statement *has finished* and the transaction is between operations (Java-side computation, JIT pause, GC pause), this timeout catches the case Postgres wouldn't. Both exist so a misconfiguration of either still bounds the request.

**Hikari `connection-timeout`.** When the pool is saturated. Hikari blocks the caller waiting for a free connection; if no connection becomes available within 10s, throws `SQLTransientConnectionException`. This surfaces under heavy load before the request ever reaches Postgres — the symptom is "503/504s with no DB activity," which is the canonical Hikari-pool-saturation signature.

Why `lock_timeout` (8s) is set lower than `statement_timeout` (10s): when both could fire, we want the *more specific* one to fire first. SQLState `55P03` says "you waited on a lock too long," which is actionable (rate-limit upstream, shrink contention scope). `57014` says "your statement ran too long," which could be that or a query plan regression or a runaway sort. Two seconds of headroom between the two means a request waiting on a lock fails with the clearer error before the umbrella `statement_timeout` swallows it. The integration test `TransactionTimeoutIntegrationTest.shouldTimeoutAndCancelIdempotencyKeyUnderLockContention` (referenced in §6) exercises the lock-wait path explicitly with a 1s `@Transactional` timeout — the request thread blocks on the foreign `SELECT ... FOR UPDATE`, surfaces 504, and the idempotency record is provably absent on re-read.

### 9.2 Why `connection-init-sql` and not `@Transactional`

The native Spring approach to "bound the transaction's wall time" is `@Transactional(timeout=10)`. We use that, but it is *not* the load-bearing bound — `connection-init-sql` is. The reason is subtle and easy to miss: `@Transactional`'s timeout is checked by Spring's `JdbcTransactionManager` only at statement boundaries. If your transaction enters a single statement that blocks indefinitely on a row lock, Spring's timer is running but has no checkpoint at which to act.

Postgres' `lock_timeout` and `statement_timeout` run *inside Postgres*, on the wait itself. They fire regardless of where in the call stack the wait is happening, because the wait is happening in Postgres. This is what closes the "blocked on lock forever" failure mode that pure-Spring timeouts can't reach.

The `connection-init-sql` runs once per Hikari connection acquisition, not once per request. The `SET` is session-level — a connection that runs `SET lock_timeout = '8s'` keeps that setting for its entire lifetime in the pool. Every request that borrows that connection inherits the bound, no per-request setup overhead. Hikari's `idle-timeout` (10 min) and `max-lifetime` (30 min) recycle connections eventually, but the init-sql runs again on the new connection. The setup cost is one extra SQL statement per ~30 minutes of connection lifetime per pool slot.

### 9.3 What surfaces as which status

`GlobalExceptionHandler` uses two complementary strategies — typed Spring DAO exception handlers for translations Spring's `SQLErrorCodeSQLExceptionTranslator` performs eagerly, and a SQLState walk for translations that fall through to the generic `DataAccessResourceFailureException`. Both routes converge on 504 for the timeout/connection family and 500 for everything else.

**Typed handlers (primary path).**

| Spring exception | Source | HTTP |
| --- | --- | --- |
| `TransactionTimedOutException` | Spring `@Transactional(timeout=N)` boundary check | 504 |
| `QueryTimeoutException` | JDBC `Statement.setQueryTimeout(N)` cancellation, or driver-level timeout | 504 |
| `CannotAcquireLockException` | Postgres SQLState `55P03` (`lock_not_available`) — what `lock_timeout` fires. Spring's PostgreSQL vendor codes (`sql-error-codes.xml`: `cannotAcquireLockCodes=55P03`) route this here. | 504 |

**SQLState walk (fallback path), in `handleResourceFailure`.**

When Spring translates a SQLException to the generic `DataAccessResourceFailureException` (no vendor-code match), the handler inspects SQLState on the cause chain:

```java
if ("57014".equals(state)            // query_canceled (statement_timeout)
        || "55P03".equals(state)     // lock_not_available (defense-in-depth; see below)
        || state.startsWith("57P")   // operator_intervention
        || state.startsWith("08")) { // connection_exception
    return true;  // → 504
}
```

The classes covered:

- **Class 57 (`operator_intervention`).** `57014` is `query_canceled` (what `statement_timeout` fires); `57P01`/`57P02`/`57P03` are admin shutdown / crash shutdown / cannot-connect-now. All map to 504 because they describe a server-side condition the client should retry past.
- **Class 08 (`connection_exception`).** Network-side failures during the statement. Same logic — retry from the client.
- **Class 55, specifically `55P03` — defense-in-depth.** On the normal Spring path `55P03` is intercepted earlier by `handleCannotAcquireLock`, so this arm rarely fires. It exists for the case where Spring's vendor-code mapping is bypassed (custom `SQLExceptionTranslator`, future driver/Spring upgrade that changes routing) — the SQLState walk still does the right thing. Matched explicitly rather than `state.startsWith("55")` because class 55 also contains non-timeout states (`55006` `object_in_use`, `55000` generic).

**Why `lock_timeout` is 504 and not 500.** From the client's perspective, both `statement_timeout` (10 s) and `lock_timeout` (8 s) are timeouts: the transaction has rolled back, no committed state was left behind, and a retry on the same idempotency key is safe. The fact that Postgres assigns them different SQLState classes (57 vs 55) is an internal vendor distinction that should not leak through to a different HTTP status code. Treating them symmetrically is the contract the rest of the stack expects — the cleanup sweep, the metric outcomes, and `RETRY_CONTRACT.md` all assume "DB-level timeout → 504, retry-safe."

### 9.4 MDC tracing

`TracingFilter` runs at `HIGHEST_PRECEDENCE`, before any filter that might log. Three slots populated:

| Slot | Source | Lifetime |
| --- | --- | --- |
| `requestId` | UUID generated per request | One HTTP request |
| `correlationId` | `X-Correlation-Id` header, or generated | Often the entire client→service→DB span |
| `clientId` | `X-Client-Id` header, when present | One HTTP request |

The log pattern in `application.yml` renders all three on every line:

```
%d{...} %-5level [%thread] %logger{36} [reqId=%X{requestId}, corrId=%X{correlationId}, client=%X{clientId:-anonymous}] : %msg%n
```

The `%X{clientId:-anonymous}` default is load-bearing. Health-check requests, public paths, and unauthenticated calls don't have a client ID, but a missing slot would render as an empty value, which an alerting pipeline that greps for `client=` would silently miss. `anonymous` is grep-stable and operationally meaningful — "this request didn't claim an identity" is a legitimate log signal.

Two non-obvious choices:

**`clientId` is captured at the tracing layer, before authentication.** A naive design would put `clientId` into MDC only after `SignatureFilter` has verified the signature, on the theory that an unauthenticated `clientId` is "untrusted." The bug that introduces is that *rejected* requests (unknown client → 403, bad signature → 401, missing key → 400) lose their `clientId` from the log line — exactly the requests an oncall debugging "why is client X getting 401s" needs to find by client ID. So we capture the *claimed* `clientId` early and accept that some log lines may carry a forged value. The `WARN/INFO` HMAC startup line (§8) tells the operator whether claims are authenticated; the log line tells them *what was claimed*.

**`MDC.clear()` runs in `finally`.** Tomcat reuses request threads; without `clear()`, the next request that the same thread handles would inherit the previous request's MDC. This is the canonical MDC-leak bug, and it's been written about at length elsewhere. There is no dedicated unit test that asserts the slots are empty after the filter completes — adding one (a filter-level test that calls `doFilter` and reads `MDC.getCopyOfContextMap()` afterwards) is a known gap and would catch any future change that drops the `finally`.

The response also echoes both IDs — `X-Correlation-Id` (the inbound or generated one) and `X-Request-Id` (always generated). Clients that retry can include the correlation ID on the retry to thread the two attempts together server-side; a client that captured the request ID can quote it in a support ticket and an operator can grep for that exact value in the logs. The integration test `TransferIntegrationTest.shouldReturnTracingHeaders` asserts both headers on the response, including the UUID format check on `X-Request-Id`.

### 9.5 Metrics: what's measured and why

Five meters, each with a different shape:

**`transfers.created`** (Counter, no tags). Number of transfers that committed successfully. The simplest aggregate. The denominator for many derived metrics — replay rate, failure rate.

**`transfers.failed`** (Counter, tagged by `reason`). Open-ended per-reason failure count. `wallet_not_found` and `insufficient_balance` are the two reasons today; new reasons will be added as new failure modes surface. Lazy registration — a reason that's never seen produces no series. The trade-off vs pre-registration is that an unused reason doesn't pollute the metric namespace, at the cost of slower first-occurrence registration.

**`transfers.execute.duration`** (Timer, tagged by `outcome`). The most operationally useful meter. Nine outcome values, *all pre-registered at construction*, with p50/p95/p99 percentiles published. The pre-registration is a small but deliberate optimization — `Timer.builder(...).register(registry)` walks the registry's deduplication path, which is constant-time but not free; doing it once at boot beats doing it on every request. The hot path is a `Map.get(outcome)` instead.

The tag has a fixed cardinality (9 outcomes), which is the right shape for Prometheus — high-cardinality tags blow up storage. We do *not* tag by `clientId` here for that reason; per-client dashboards would have to derive from access logs, not from this metric.

**`idempotency.replays.total`** (Counter, no tags). Replay rate. The watch-this metric for client retry storms — if `idempotency.replays.total` is climbing faster than `transfers.created`, callers are over-retrying.

**`idempotency.advisory_lock.rejections`** (Counter, no tags). Number of times `pg_try_advisory_xact_lock` returned `false`. The metric description in code says it bluntly: *almost always a true concurrent retry, but a sustained non-zero rate at low traffic could also mean Hashing.toLockId hash collisions and is worth investigating.* This is the canary metric for §3.4 — the 64-bit hash fold has a theoretical collision rate, and this counter is how you'd notice it.

The percentile choice (p50, p95, p99) follows the standard SLO triad. p50 is the typical-user experience; p95 is the "most users see better than this"; p99 is the tail that drives outage characterization. p99.9 was considered and rejected — the noise floor at low request volumes makes p99.9 estimates unreliable, and a service at our scale produces enough p99 samples per minute to be statistically meaningful while p99.9 lags by minutes.

### 9.6 Where alerts should live

Not all metrics deserve alerts. Three are operationally load-bearing:

- **`transfers.execute.duration{outcome="error"}`** — anything tagged `error` is unexpected. A non-zero count is an investigation. The catch-all in `TransferController` writes to this.
- **`transfers.execute.duration{outcome="transient_conflict"}`** — the 503 retry-budget-exhausted path. Sustained means the system is contended past its retry capacity; either tune the retry budget (`transfer.retry.max-attempts`) or fix the upstream contention.
- **`idempotency.advisory_lock.rejections`** — any spike at low traffic is investigable per §3.4.

Two are watch-but-don't-alert:

- **`idempotency.replays.total` / `transfers.created` ratio** — a rising replay rate is a client behaviour signal, not necessarily a server problem. Dashboard, don't page.
- **`transfers.execute.duration{outcome="hash_conflict"}`** — caller bug. Worth surfacing to the team that owns the caller, not actionable on the server side.

The `success` and `replay` outcomes don't deserve alerts on their own — they're the denominator for ratios.

### 9.7 Spring Boot Actuator

`management.endpoints.web.exposure.include: health,info,metrics,prometheus` exposes the standard four:

- `/actuator/health` — liveness/readiness for the orchestrator. `show-details: always` because the operational benefit (being able to tell which dependency is down) outweighs the information leakage in a private network. A public-facing deployment should set `show-details: when-authorized` instead.
- `/actuator/info` — build-info / git-info. Useful at incident response — "what version is deployed in this env?" without SSHing.
- `/actuator/metrics` — Micrometer's HTTP-friendly format. Useful for ad-hoc "let me check this metric right now" queries.
- `/actuator/prometheus` — the scrape endpoint. The deployment's Prometheus instance hits this on whatever schedule it's configured for (typically 15-30s).

The application name (`spring.application.name`) is registered as a global Micrometer tag (`management.metrics.tags.application`), so a Prometheus that scrapes multiple deployments can disambiguate by `application` label without per-meter wiring.

### 9.8 Alternatives rejected

**Per-request `SET LOCAL lock_timeout`.** Run the SET inside each transaction, scoped to that transaction (`SET LOCAL` instead of `SET`). Rejected because every transaction would emit two extra round-trips (one for `SET LOCAL lock_timeout`, one for `SET LOCAL statement_timeout`) for a value that is identical for every transaction in a deployment. `connection-init-sql` does it once per connection acquisition; the cost is amortized over hundreds of requests.

**Per-tenant timeout policy.** Some clients are slower than others; what if their timeouts were configurable? Rejected because timeouts are a *server-side* property, bounding what the server is willing to wait. A "slow client" gets timeouts based on the server's own constraints; the client's expectations don't change the server's capacity.

**Application-level wall-clock timer instead of Postgres-side.** Spawn a watchdog thread that interrupts the JDBC call after N seconds. Rejected because interrupting a JDBC call leaves the underlying connection in an indeterminate state — Postgres may or may not have started the work; cleanup is unreliable. `lock_timeout` and `statement_timeout` are server-side; Postgres knows exactly what to roll back.

**No metrics; rely on access logs.** Tempting for "small projects." Rejected because access logs are unindexed (in our deployment) and aggregate queries on them are minutes-stale; metrics are sub-second. The access log is for forensics, the metric is for alerts.

**OpenTelemetry / distributed tracing instead of MDC.** Tempting for "real-world." Rejected within the assignment scope — MDC is enough for single-service tracing; distributed tracing needs an upstream that propagates the span context, and we don't have one. The MDC layer is structured to be replaceable: an OTel-aware filter can populate MDC slots from the OTel context with no changes to the rest of the codebase.

**Metric pre-registration for *every* meter.** We pre-register the timer (9 known outcomes) but not the failure counter (open-ended reasons). Rejected for the failure counter because adding a new reason should not require both a code change in `TransferMetrics` *and* a config update — the failure path is a place we want to be permissive.

### 9.9 Operational consequences

- **A 504 spike with no DB CPU correlation = lock contention.** `lock_timeout` is firing; queries are waiting, not running. Look at `pg_stat_activity` for waiters.
- **A 504 spike with DB CPU correlation = slow query.** `statement_timeout` is firing; queries are running too long. Look at the query plan.
- **A 503 spike = retry budget exhausted.** Different mechanism (`TransferService.executeTransfer` loop, §6); not a Postgres-level timeout. Look at `idempotency.advisory_lock.rejections` for signal of conflict-then-rollback storms.
- **A connection-pool warning in logs (`leak-detection-threshold: 30s`) is a code bug.** Hikari thinks a connection has been borrowed for >30s without return — usually a missing `close()` somewhere or a transaction that never committed. The threshold is set conservatively at 30s because legitimate transactions never exceed `statement_timeout` (10s) plus some processing buffer; anything past 30s is a leak.
- **The `WARN/INFO` startup lines (§8) plus the `SignatureFilter` mode are the first triage step** for "is this configured correctly?"
- **MDC slots cannot leak across requests** because of `MDC.clear()` in `finally`; if you ever see one (an old `clientId` on a fresh request), the bug is a missing `finally` in a custom filter, not in `TracingFilter`.

### 9.10 Tests that lock this in

- `TransactionTimeoutIntegrationTest.shouldTimeoutAndCancelIdempotencyKeyUnderLockContention` — overrides `transfer.transaction.timeout-seconds` to 1s, holds `SELECT ... FOR UPDATE` on `wallet_1` from a separate connection, asserts the API request returns 504, the wait was at least 1s and under 5s (catches both fail-open regressions and accidental timeout-bumping regressions), and the idempotency record was rolled back (`findById` returns empty). Exercises the framework-timer path — the 1s `@Transactional` budget fires before the 8s default `lock_timeout`.
- `LockTimeoutIntegrationTest.lockTimeoutReturns504` — the inverse configuration: `lock_timeout=200ms`, `statement_timeout=30s`, `@Transactional=30s`, so the DB-side `lock_timeout` deterministically fires first. Asserts 504 (not 500 — guards against the typed-handler regression that prompted this fix), duration in `[200ms, 5s)`, and idempotency record absent on re-read. Pinned together with `TransactionTimeoutIntegrationTest`, the two cover the full §9.1 ordering matrix end-to-end.
- `GlobalExceptionHandlerTest` — 8 unit tests on the SQLState→HTTP mapping (§6.6, §9.3). Locks down both translation arms in-process so a regression on either branch is caught without spinning up Testcontainers.
- `TransferIntegrationTest.shouldReturnTracingHeaders` — sends an inbound `X-Correlation-Id`, asserts the response echoes it unchanged in `X-Correlation-Id` and emits a fresh UUID in `X-Request-Id` (parsed via `UUID.fromString` to enforce shape).
- A direct unit test of `TransferMetrics.recordExecutionDuration` round-tripping through the registry for each `OUTCOME_*` value does not exist; metric correctness is exercised opportunistically via the integration suite (every transfer / replay / failure path increments the histogram), but a focused unit test would be a tighter regression boundary. Known gap.
- A smoke test against `/actuator/prometheus` asserting the meter names appear in the scrape output does not exist; the endpoint exposure is currently relied on transitively from the Spring Boot defaults. Known gap — adding one would guard against a future framework upgrade silently breaking the exposure.

---

## 10. Lifecycle

← Overview: [`DESIGN.md` §10](./DESIGN.md#10-lifecycle)

Idempotency is the only piece of state in this service that *grows monotonically* under healthy operation. Every successful transfer adds a row; every failed transfer adds a row; every replay touches a row. Wallets and transfers are also append-only, but each is bounded by external state (wallets are seeded; transfers are bounded by client traffic that has business value). Idempotency records are bounded by *retry traffic*, which has no upper limit — a misbehaving client can produce millions of identical replays, and the table will absorb them all unless something cleans up. This section walks through what eviction looks like, why the leader-election is the way it is, and why several "obviously simpler" alternatives were rejected.

### 10.1 The retention policy

Records older than 72 hours are eligible for eviction. The "older than" predicate uses `first_seen_at`, not `last_seen_at`:

```sql
DELETE FROM idempotency_records
WHERE (client_id, idempotency_key) IN (
    SELECT client_id, idempotency_key FROM idempotency_records
    WHERE first_seen_at < ? -- threshold
    ORDER BY first_seen_at
    LIMIT ?                  -- batch size
)
```

The composite-key tuple (`(client_id, idempotency_key)`) is the actual primary key of `idempotency_records` — there is no synthetic `id` column. The two-table-name pattern (`DELETE … WHERE (pk-tuple) IN (SELECT …)`) limits the transaction to a known number of rows even when the predicate matches millions; without the inner LIMIT, a single DELETE could grab the entire eligible set and run for minutes. The inner ORDER BY ensures deterministic batching — successive batches drain the oldest records first, so a slow eviction makes monotone progress against the backlog rather than oscillating across the threshold.

Two design choices baked into the predicate:

**`first_seen_at` over `last_seen_at`.** This is the load-bearing choice for retention. With `last_seen_at`, a frequently-replayed key extends its own lifetime indefinitely — every replay touches the timestamp, every replay resets the eviction clock. A pathological client that replays the same key once an hour would keep the record alive forever, even if the original transfer is from 2019. With `first_seen_at`, the record's lifetime is bounded from the moment it was created. After 72 hours it's evictable regardless of how often it's been replayed.

The trade-off is that a heavily-replayed key disappears at exactly 72h, even if its last replay was minutes ago. Replays after eviction execute as fresh requests — a new INSERT, a new (possibly identical) transfer. For our workload that's correct: the assignment doesn't bound the replay window, but 72 hours is far longer than any reasonable client retry horizon. A client that retries 3 days after the original isn't really "retrying"; it's making a new request. Capping the dedup window at 72h matches that intuition.

**72 hours as the magic number.** Long enough that a slow client retry still hits the cached response (covers weekend incidents, multi-day backlogs, paused worker queues). Short enough that the table doesn't grow unbounded — at modest traffic of 1M transfers/day, 72 hours of retention is ~3M rows, which fits comfortably in Postgres-side memory and keeps query plans tight. A deployment with different traffic shape can tune `idempotency.cleanup.retention-hours` without code changes.

### 10.2 The supporting index

V2 adds an explicit index for the eviction predicate:

```sql
CREATE INDEX idx_idempotency_first_seen_at
    ON idempotency_records (first_seen_at);
```

Without this, the eviction query would full-scan the table on every run — the PRIMARY KEY is `(client_id, idempotency_key)`, which doesn't help a range scan over `first_seen_at`. A full scan over a multi-million-row table on the hourly cron would lock the table for seconds, hold a long transaction open, and almost certainly trip `statement_timeout` (10s) before completing.

The index is one-column, ascending. The eviction's `WHERE first_seen_at < ?` is a left-bounded range scan, which Postgres serves by walking the leftmost leaves of the B-tree; the inner `ORDER BY first_seen_at LIMIT N` reads the first N entries directly without sorting. The index is also small — 8 bytes per `TIMESTAMPTZ` plus the row pointer — so its maintenance cost on every INSERT is negligible.

Note that we did *not* create a partial index (`WHERE status IN ('COMPLETED', 'FAILED')`). Two reasons. First, a partial index excludes IN_PROGRESS rows, but those rows are short-lived (they convert or roll back within a second), so excluding them only saves a few entries. Second, the eviction does not filter by status — a stranded IN_PROGRESS row from a server crash *should* be evicted by retention, otherwise it would persist forever. Including all statuses in the index is the correct semantic.

### 10.3 The scheduler

```java
@Scheduled(cron = "${idempotency.cleanup.cron:0 0 * * * *}")
public void cleanupExpiredRecords() { ... }
```

Default cron: top of every hour. `Spring's @Scheduled` runs the method on a single managed thread per scheduled task; no two ticks of the same cron overlap, even if a tick takes longer than the cron period. The `Spring TaskScheduler` runs in its own thread pool, separate from the Tomcat request handlers — eviction work doesn't compete with request handling for HTTP threads.

A non-obvious property: the cron runs *on every replica*. If three instances of the service are deployed behind a load balancer, all three fire `cleanupExpiredRecords()` at the top of the hour. Without coordination, they would all DELETE the same rows, contend on the same B-tree pages, and either deadlock or duplicate work. That's where the leader lock comes in (next subsection).

The `Spring's @Scheduled` cron only runs when the application is up; there is no missed-execution catch-up. A 90-minute outage during which two ticks would have fired produces zero ticks, and the next post-recovery tick finds 90 minutes more rows than usual. The `maxBatchesPerTick` ceiling (next subsection) bounds how much work that next tick can do in one swing; the remainder picks up on subsequent ticks. So a 90-minute outage means a few hours of slow drain, not a permanent backlog.

### 10.4 Leader election via transaction-scoped advisory lock

The eviction work runs under `pg_try_advisory_xact_lock(CLEANUP_LEADER_LOCK_ID)`:

```java
int deleteOneBatch(OffsetDateTime threshold) {
    Integer deleted = batchTransactionTemplate.execute(status -> {
        if (!repository.tryAdvisoryXactLock(CLEANUP_LEADER_LOCK_ID)) {
            log.debug("Skipping idempotency cleanup batch — another instance holds the leader lock");
            return 0;
        }
        return repository.deleteOlderThan(threshold, batchSize);
    });
    return deleted == null ? 0 : deleted;
}
```

`pg_try_advisory_xact_lock` is non-blocking — it returns `true` if the caller acquired the lock, `false` if anyone else (any other connection in the same Postgres instance) holds it. Transaction-scoped means it auto-releases on commit or rollback. The lock id `CLEANUP_LEADER_LOCK_ID` is computed once at class load via `Hashing.toLockId("wallet:idempotency-cleanup-leader")` — a stable 64-bit integer; the seed string is a namespace token to avoid collision when this Postgres instance is shared with other applications.

The leader-election semantics: at the top of the hour, three replicas all enter `deleteOneBatch`. Each opens its own transaction and tries the advisory lock. The *first* to win the lock proceeds with the DELETE; the other two get `false`, log a debug message, return zero deletions, and short-circuit out of the per-tick loop (the outer loop sees zero deleted and breaks). Their transactions commit (or roll back; it doesn't matter) and they idle until the next tick. The leader, meanwhile, holds its lock for one DELETE statement, commits, releases — at the *next* batch the leader-election runs again, and a different replica might win.

Two reasons for the design:

**The lock is per-batch, not per-tick.** The leader is re-elected for every batch. If the original leader stalls between batches (long GC pause, slow disk, network blip), the next batch can fall to a different replica. There is no long-lived "leader of the cleanup process" — only a leader of *this batch*. The cost is a small extra round-trip per batch; the benefit is no failure mode where one replica's slowness prevents progress.

**Lock id collision is bounded.** `Hashing.toLockId` collisions are theoretically possible (§3.4), but the only way a collision matters here is if some *other* application using the same Postgres instance happens to acquire a lock with the same fold-to-64 hash. The seed string `"wallet:idempotency-cleanup-leader"` makes this unlikely; a deployment that shares a Postgres with a known peer should change the seed.

### 10.5 Why transaction-scoped, not session-scoped

The session-scoped variant — `pg_try_advisory_lock` paired with an explicit `pg_advisory_unlock` in a `finally` block — was the first design and the first one that broke.

The breakage path: HikariCP returns connections from a pool. Each `dsl.execute(...)` call acquires a connection from the pool, runs the statement, and returns it. *Different statements within the same Java method can land on different physical connections.* If the acquire (`pg_try_advisory_lock`) lands on connection A and the unlock (`pg_advisory_unlock`) lands on connection B, the unlock is a no-op against B (which doesn't hold the lock) and connection A keeps the lock until it cycles out of the pool — typically up to `max-lifetime` (30 minutes).

Concretely, a 30-minute pin would mean: leader replica acquires the lock at 14:00, batches finish at 14:00:05, `unlock()` runs but on a different physical connection, original connection still holds the lock and sits idle in the pool for up to 30 minutes. The 15:00 cron fires, every replica including the original tries `pg_try_advisory_lock`, all fail (connection A still holds it). The hourly cron silently skips ticks until connection A is recycled.

The transaction-scoped variant avoids this entirely. `pg_try_advisory_xact_lock` releases automatically on COMMIT or ROLLBACK of *the transaction*, regardless of which connection runs the next statement. Because the entire `deleteOneBatch` work is wrapped in `batchTransactionTemplate.execute(...)`, the lock and the DELETE share one transaction; the COMMIT releases the lock and the DELETE simultaneously. There is no explicit `unlock` to mis-target.

This is documented at length in the class Javadoc precisely because a future maintainer who notices "we're using xact lock here, why not session?" needs to find the answer in the code, not in tribal knowledge.

### 10.6 Why `TransactionTemplate`, not `@Transactional`

A second subtle Spring trap. The natural place to put `@Transactional(propagation = REQUIRES_NEW)` is on `deleteOneBatch`, called from the loop in `cleanupExpiredRecords`. That would be wrong.

Spring's `@Transactional` is a proxy-based aspect. The proxy intercepts external calls but *not* self-invocations through `this`. `cleanupExpiredRecords` and `deleteOneBatch` are both methods on the same class; calling `deleteOneBatch(threshold)` from the loop calls *the actual method*, not the proxy, and the `@Transactional` advice is never applied. Spring would log nothing — no warning, no error, no opt-in flag — the annotation is just silently inert.

The breakage if this were missed: `deleteOneBatch` runs *without* an explicit transaction. The default JDBC behaviour is auto-commit, which means each SQL statement is its own implicit transaction. The advisory lock is acquired in transaction-1 (the SELECT for the lock function), the DELETE runs in transaction-2 (a brand-new transaction), and so on. The advisory lock releases at the end of transaction-1 — *before* the DELETE — and any other replica starting its own batch can acquire the lock. Both replicas DELETE; the dual-runner race is back.

`TransactionTemplate.execute(...)` opens the transaction *at the call site*, which means the lambda runs inside an explicit transaction regardless of whether the surrounding method is annotated. The boundary is visible to a reader (you can see `batchTransactionTemplate.execute(status -> { ... })` in the code) and the transaction encompasses both the lock acquisition and the DELETE. The lock survives until commit; the lock and the DELETE share the same lifetime.

A test bypass note: `deleteOneBatch` is package-private rather than private so tests can call it directly. Because `TransactionTemplate` opens the transaction at the call site, the tested code path matches the production code path — there is no proxy to bypass and no annotation to find missing.

### 10.7 Batching and per-batch failure tolerance

Two interacting bounds shape per-tick work:

| Setting | Default | Effect |
| --- | --- | --- |
| `batchSize` | 1000 | Rows deleted per DELETE statement |
| `maxBatchesPerTick` | 50 | Hard ceiling on batches per scheduled tick |

Per-tick worst case: `50 × 1000 = 50,000` rows. Each batch holds the advisory lock for the duration of one DELETE — typically tens of milliseconds against the indexed predicate. The total tick time is bounded at ~5 seconds even at the ceiling, comfortably below `statement_timeout`.

At default settings, a 100-million-row backlog (which would happen only after extreme misconfiguration) drains at 50,000 rows/hour = 1.2M/day. Recovery from that scale of backlog needs `maxBatchesPerTick` widened. Doing it via env var means an oncall can dial the cleanup up during the drain (`IDEMPOTENCY_CLEANUP_MAX_BATCHES_PER_TICK=500` would 10× the rate) without a redeploy, and dial it back down after. That config surface is exactly the operational lever the comment in the code describes.

The per-batch error handling is also load-bearing:

```java
for (int batch = 0; batch < maxBatchesPerTick; batch++) {
    int deletedInBatch;
    try {
        deletedInBatch = deleteOneBatch(threshold);
    } catch (Exception e) {
        log.error("... continuing with next batch", e);
        continue;  // do NOT break — the next batch may succeed
    }
    if (deletedInBatch == 0) break;
    totalDeleted += deletedInBatch;
}
```

A transient error on one batch (a deadlock with a concurrent INSERT, a 1-second blip in connection availability, a single-batch lock_timeout) should not skip the remaining `maxBatchesPerTick - n` healthy batches. The `continue` on exception lets the loop keep trying, with the next iteration's transaction being completely independent of the failed one. If the failure is *persistent* — say the table is genuinely unreachable — every batch in the tick errors, the loop exits at `maxBatchesPerTick`, and the next cron tick re-runs from scratch. The cron's natural cadence is its own back-pressure.

The break on `deletedInBatch == 0` covers two cases: there are no rows older than the threshold (the steady state — most ticks finish in one batch), or another replica is the leader (we got `false` from the lock and returned 0). In the former we're done; in the latter we *let the leader run* — there is no value in this replica spinning the loop just to acquire-and-fail repeatedly.

### 10.8 What happens at a node restart

A scheduled cleanup tick that's mid-execution when the JVM is killed simply stops. Whatever transaction was open is rolled back by Postgres on connection close — the advisory lock releases, any in-progress DELETE rolls back, no rows are evicted from that tick. The next replica's tick (or the same replica after restart) fires at the next cron and starts from scratch. There is no checkpoint, no resume, no "continue from row X" semantics. The only observable effect is that the row-count-deleted-per-tick log line for the killed tick is missing.

Stranded IN_PROGRESS rows from server crashes during a transfer (not during cleanup) are *also* evictable by this same cleanup. After 72 hours, an IN_PROGRESS row counts as "older than retention" by `first_seen_at` and gets DELETEd alongside COMPLETED/FAILED rows. There is no separate "stale IN_PROGRESS sweep" — the retention sweep covers it. A future contributor who wants more aggressive IN_PROGRESS eviction (say, 1 hour) can add a status filter to a separate, faster cron without touching the main retention path.

### 10.9 Alternatives rejected

**`pg_advisory_lock` (session-scoped) with explicit unlock.** Already covered (§10.5). The connection-rotation hazard makes the unlock unreliable.

**A dedicated leader-election library (Curator / etcd / ZooKeeper).** Adds an entirely new infrastructure dependency for one process: an hourly cleanup of idempotency rows. Postgres advisory locks already provide the property. A new dependency is justified only if it does something the database can't, and Postgres's advisory locks meet our needs exactly.

**External cron (Kubernetes CronJob hitting an admin endpoint).** Tempting because it inverts the responsibility — "the platform schedules; the service responds." Rejected because:
- The admin endpoint would need to be authenticated (one more attack surface).
- The platform's cron and the application's deployment lifecycle would couple — a redeployment that didn't include the cron config would silently disable cleanup until someone noticed.
- The leader election we currently get from advisory locks would still be needed (because `kubectl create cronjob` produces one Pod per execution, but a misconfiguration could produce two).
- The current `@Scheduled` is one annotation and runs out-of-band of HTTP requests; the migration cost to external cron is higher than the operational benefit.

**A single unbounded `DELETE WHERE first_seen_at < ?`.** Rejected for the long-running-transaction hazard mentioned in the class Javadoc: WAL bloat, statement_timeout trip, locks held for the duration. Batching is the right shape.

**Soft-delete (`deleted_at` column instead of DELETE).** Tempting for "audit trail." Rejected because *idempotency records are not audit data* — they're operational state, deduplication ledger entries. The audit trail for transfers lives in `transfers` and `ledger_entries`, both of which are immutable by trigger. Soft-deleting idempotency records would just trade unbounded growth of live rows for unbounded growth of soft-deleted rows.

**TTL-based table partitioning (Postgres declarative partitioning).** Partition `idempotency_records` by `first_seen_at` weekly; drop old partitions wholesale. Genuinely simpler at scale and a strong choice for traffic 100× ours. Rejected within the assignment scope as overengineering — partitioning adds DDL complexity and changes the application's interaction with the table (insert routing, FK behaviour, query planning). At our default 1M-transfers/day order of magnitude, the batched DELETE is operationally fine. A future scale-up can move to partitioning without changing the application code, only the migration files.

**Sweep `transfers` and `ledger_entries` too.** Already covered in §11. Transfers and ledger entries are audit data; we do not delete them on retention. Doing so would invert the audit invariant (§5).

**Aggressive IN_PROGRESS-only sweep (e.g. every 5 minutes for rows >5min old).** Rejected because IN_PROGRESS is genuinely short-lived in healthy operation — `statement_timeout = 10s` bounds the maximum legitimate duration. A row stuck IN_PROGRESS past 10 minutes is almost certainly the result of a server crash, and the conflict-then-rollback retry path (§6) already handles the corresponding race for new requests reaching the same key. The 72-hour retention sweep is enough.

### 10.10 Operational consequences

- **Cleanup running normally is invisible.** The successful path logs at INFO with the count, only when `totalDeleted > 0`. Steady-state ticks that find no work log nothing at INFO.
- **Cleanup failing is visible.** Per-batch errors log at ERROR with the exception. A grep for `Failed to execute background idempotency records cleanup batch` is the alert.
- **The leader-skipped path logs at DEBUG.** Two replicas that competed for the lock and lost will *not* emit INFO/WARN — that's the design (it's a normal occurrence). If you're debugging "is my replica running cleanup at all?" turn on DEBUG for `IdempotencyCleanupService`.
- **`maxBatchesPerTick` is the operational throttle.** Default 50 keeps each tick under ~5 seconds of work. Raise it during a known backlog drain; revert when the drain completes.
- **Retention is symmetric across all replicas.** Lower it on one replica only and you get a split-brain on which records are evictable; the leader-elected replica's setting decides each tick. Always change retention via the env var that's deployed to every replica.
- **Restarting a replica mid-eviction is safe.** The transaction rolls back; no half-state persists.

### 10.11 Tests that lock this in

- `IdempotencyCleanupServiceTest.shouldInvokeRepositoryDeleteOlderThan` — leader lock acquired, repository returns 5 deleted then 0 on the next batch (loop drains and exits); the test verifies two `deleteOlderThan` invocations with the configured batch size and asserts the threshold passed to the repository is roughly `now() - retention-hours` (within a 5-second clock-drift tolerance).
- `IdempotencyCleanupServiceTest.shouldSkipWhenAnotherInstanceLeads` — `tryAdvisoryXactLock` returns false (another replica holds the lock); the test asserts `deleteOlderThan` is never called for that tick — leader election is honoured.
- `IdempotencyCleanupServiceTest.shouldNotPropagateOnFailure` — repository raises a runtime exception on the DELETE; the test asserts the service swallows it without re-throwing, so one bad tick does not poison the `@Scheduled` chain.
- `IdempotencyCleanupServiceTest.shouldOpenActiveTransactionAroundDeleteBatch` — regression guard for the proxy-self-invocation pitfall: asserts `TransactionSynchronizationManager.isActualTransactionActive()` returns `true` at both the lock acquisition and the DELETE call, so the xact-scoped advisory lock survives past the first statement.
- `IdempotencyCleanupServiceTest.shouldRejectInvalidBatchSize` — constructor-validation: `batchSize=0` fails at construction with `IllegalArgumentException`, surfacing config errors at startup instead of at the first scheduled tick.
- The boundary cases — a record *exactly at* the threshold surviving, batch-size and max-batches-per-tick caps observed against real seeded data, leader re-acquisition after the foreign holder commits — are not exercised by integration tests directly. Adding a Testcontainers-backed cleanup test that seeds rows on both sides of the threshold is a known gap.

---

## 11. What we deliberately did not build

← Overview: [`DESIGN.md` §11](./DESIGN.md#11-what-we-deliberately-did-not-build)

The `DESIGN.md` version of this section is a list of "no"s with one-line reasons. The deep-dive version walks through each one's deliberation: what was on the table, what would have to change to make it a yes, and what the failure mode of including it badly would look like. Read this as the *boundary* of the system — anything past these lines either belongs to a layer that sits in front of this service, or is a deliberate scope decision driven by the assignment.

### 11.1 Wallet ownership / per-wallet ACLs

**Rejected because:** the assignment doesn't ask for it, and there is no identity model in the prompt to root an ACL against.

**What was on the table.** Add a `wallet_owner` column to `wallets` and check `wallet_owner == X-Client-Id` at the start of every transfer. The check would surface as 403 if violated.

**Why we didn't.** "Ownership" without a clear identity model is *worse* than no ownership. The X-Client-Id header is a namespace; with HMAC enabled it's also authenticated. But authentication is not authorization — knowing that the caller is who they claim to be is not knowing that the caller is *allowed to move money between wallets A and B*. A naive `wallet_owner == clientId` check enforces a 1:1 ownership model that doesn't fit most real systems (joint accounts, treasury wallets, system accounts, escrow), and rolling back the check later is harder than not adding it now.

**What would have to change to add it.** A schema migration adding `wallet_owner VARCHAR(64) REFERENCES clients(id)` to `wallets`, an enforcement check in `TransferExecutionService.lockBothWallets`, and a richer model than 1:1 ownership for the realistic cases. The last one is the hard part — designing a permissions model that handles delegation, multi-owner wallets, and read-vs-write distinctions is a project larger than this entire service.

**The recommended replacement.** A gateway in front of this service that enforces caller-side authorization rules. The wallet service stays small and the business policy lives where the business is.

### 11.2 Multi-currency

**Rejected because:** the schema is `BIGINT amount`, single denomination. Adding currencies without an FX policy, rounding rules, and a settlement model is worse than not having it.

**What was on the table.** Add `currency_code VARCHAR(3)` to `wallets` and `transfers`. Reject mixed-currency transfers; or accept them with FX rates and rounding.

**Why we didn't.** Two non-trivial decisions blow this scope up. *FX rate sourcing*: which rate, when, from whom, with what staleness tolerance? *Rounding*: integer minor units (cents) vs decimal, and what to do with the lost fraction (round half-up, banker's rounding, or stash to a "rounding" wallet?). Each has been the subject of multi-quarter projects at financial systems; doing them badly creates audit-grade bugs that are extremely expensive to find later.

**What would have to change to add it.** Schema migrations on `wallets` and `transfers` to carry `currency_code`. A new `fx_rates` table with effective-from/to timestamps. A rate-resolution service for mixed-currency transfers. A rounding policy that's explicit, testable, and consistent with whatever the caller's regulator requires. None of this is more code than what we've already written; it's *more thought*, and the thought is rooted in domain decisions outside this codebase.

**The recommended replacement.** Stay single-denomination per service instance. Run a separate instance per currency, with the FX layer above as a callable service. This is the pattern used by most ledger-as-a-service products in production.

### 11.3 Async retry queue

**Rejected because:** the synchronous `503 + Retry-After` contract preserves the stateless property of the service.

**What was on the table.** Instead of returning 503 when the retry budget exhausts, enqueue the request to an internal worker queue and return 202 (Accepted) with a status URL. The worker re-tries on its own schedule and updates the request record asynchronously.

**Why we didn't.** Three coupled costs. *State explosion*: the service becomes stateful — there's now a queue to manage, a worker pool to schedule, a status table to provide read-after-enqueue consistency. *Operational complexity*: every deploy now has to coordinate worker drain with new code rollout. *Contract change*: a 202 with a callback URL is a fundamentally different API shape than a 201/200/422/etc. service. Clients have to be designed for it from day one.

The 503-with-Retry-After contract is *also* an async retry — but with the retry happening on the *client side*, where it costs nothing to operate. The server stays stateless; the client's retry is a familiar HTTP pattern; the failure mode if the client gives up is "the transfer didn't happen," which is easier to reason about than "the transfer was queued, and may or may not have happened, and the only way to find out is poll."

**What would have to change to add it.** A queue (database-backed outbox or external broker), a worker process, a status endpoint, a different response shape, and a story for queue draining at deployment. This is a different service architecture, not a feature.

**The recommended replacement.** None — `503 + Retry-After` is the right answer for this service shape.

### 11.4 Soft-delete on wallets

**Rejected because:** the `BEFORE DELETE` trigger blocks DELETE, and `WalletService.getTransferHistory`'s short-circuit relies on the append-only invariant (§5.4).

**What was on the table.** Replace the trigger's blanket DENY with a `deleted_at` column update; treat `deleted_at IS NOT NULL` as deleted in queries.

**Why we didn't.** The optimization in `getTransferHistory` ("if a transfer row exists, the wallet exists, skip the explicit existence check") relies on `wallets` being literally append-only. Soft-delete breaks this — a transfer row can reference a soft-deleted wallet, and the existence check is no longer redundant. Lifting the optimization would slow down a hot read path. Adding "and not soft-deleted" semantics across every wallet read site is a wide refactor with subtle correctness traps (does a soft-deleted wallet's balance count? what about its outbound history?). The trigger's hard refusal is louder, simpler, and avoids the entire question.

**What would have to change to add it.** A schema migration adding `deleted_at TIMESTAMPTZ`, replacing the trigger, updating every wallet-read site to filter (or returning the trigger but allowing soft-delete via an explicit `INSERT INTO deleted_wallets (wallet_id, ...)` audit table), and a decision on how soft-deleted wallets interact with transfer history. The decision is the load-bearing piece — and it's a product decision, not a code one.

**The recommended replacement.** If wallets need to be "decommissioned," post a compensating transfer that empties the balance and mark the wallet "frozen" via a status column rather than deleting the row. Frozen wallets reject new transfers (a service-layer check) but preserve audit history.

### 11.5 Bulk transfers

**Rejected because:** `POST /transfers` is single-transfer, and a bulk variant changes the concurrency and error contracts substantially.

**What was on the table.** A `POST /transfers/bulk` endpoint that accepts an array of transfer requests and returns either all-or-nothing or per-item-status semantics.

**Why we didn't.** The two semantic options each have real costs. *All-or-nothing*: every wallet touched by the bulk has to be locked in the same transaction, and the lock-order proof (§4.4) extends to the *entire set* of wallets in the bulk. The transaction's scope grows with the bulk size; under contention, a 1000-item bulk holds 1000 wallet locks for the duration, blocking every concurrent single transfer that touches any of those wallets. *Partial success*: each item gets its own transaction, success/failure recorded per-item; the response shape is a per-item array. Idempotency now has to be defined at the bulk level (one key for the whole bulk) or per-item (an array of keys, one per item) — both have failure modes (caller passes 1000 items with 1 dup vs caller passes 1000 keys, only 998 unique).

**What would have to change to add it.** A new endpoint with a different request/response schema, a re-thought lock acquisition strategy (probably a dedicated path that locks N wallets in one shot and either succeeds for the set or fails the bulk), per-item error reporting, and a per-item idempotency model. The single-transfer service stays untouched — this is a sibling endpoint with a parallel implementation.

**The recommended replacement.** None within this service. The simplest external replacement is a client-side fanout: caller iterates and calls `POST /transfers` once per item. Backpressure is handled by the caller's connection pool; partial failure is handled by the caller's error handling. This is what most callers actually need.

### 11.6 A `markFailed` UPDATE

**Rejected because:** the row to update doesn't exist by the time failure runs.

**Mechanics covered in §7.2.** The IN_PROGRESS row was rolled back with the executing transaction; an UPDATE against an absent row returns 0 rows affected and silently no-ops.

**Why this is in the "deliberately did not build" list anyway.** Because it is the most natural-looking design choice that's wrong, and listing it here makes the "no" explicit for any future reader who might think they're "simplifying" the recorder by adding such a method. The repository deliberately exposes no `markFailed`; adding one would require deleting that comment from `IdempotencyRecordRepository`.

### 11.7 A session-scoped advisory lock

**Rejected because:** Hikari connection rotation makes the explicit unlock unreliable.

**Mechanics covered in §10.5.** Acquire on connection A, unlock on connection B, lock pinned for up to `max-lifetime` (30 minutes), hourly cron silently skipped during the pin window.

**Why this is in the "deliberately did not build" list anyway.** Same reasoning as §11.6 — it is the obvious choice that's wrong, and explicit documentation prevents the regression. The class Javadoc for `IdempotencyCleanupService` repeats the rationale at length so a future maintainer who comes in saying "why are we using xact lock, session lock would be cleaner" finds the answer immediately.

### 11.8 A transfer reversal endpoint

**Rejected because:** the ledger is immutable by trigger, and reversal-by-edit would erase audit evidence.

**What was on the table.** A `POST /transfers/{id}/reverse` endpoint that creates a compensating entry — moves the same amount back from `to_wallet` to `from_wallet`, with a reference back to the original `transfer_id`.

**Why we didn't ship this in scope.** Two reasons. First, it's a feature request beyond what the assignment asks for. Second, the right shape for it is *not* a special endpoint; it's just *another POST /transfers* with a `reversal_of` reference column on `transfers`. The schema is ready for this — adding a nullable `reversal_of UUID REFERENCES transfers(id)` is a one-line migration. The controller just needs to look it up and let the existing transfer machinery do the rest. There's no new concurrency model and no new ledger model. We left it out because the assignment doesn't ask for it, not because it's hard.

**What would have to change to add it.** A migration adding `reversal_of`, a controller method that resolves the original transfer and constructs the compensating request, idempotency-key handling that takes either an explicit key from the caller or generates one deterministic on `original_transfer_id + "-reversal"`. A few hundred lines of code, all of which fits inside the existing model.

### 11.9 Per-tenant rate limits

**Rejected because:** rate-limiting belongs at the gateway, not in the wallet service.

**What was on the table.** Token-bucket per `X-Client-Id`, enforced in a filter; limit configurable per-tenant.

**Why we didn't.** Rate-limiting requires a shared store (otherwise a multi-replica deployment lets a caller burst by `N × limit`), which means another infrastructure dependency. It also requires a decision on what to rate-limit *by* — request count, byte rate, transfer amount? — and the right answer almost always lives at the layer that sees more than one service (the gateway), not inside one service.

**What would have to change to add it.** A shared rate-limit store (Redis is the canonical answer), a filter that consults it, a per-tenant configuration surface, and an answer for "what happens when the rate-limit store is down?" (fail open or fail closed?). All solvable, but a project on its own.

**The recommended replacement.** API gateway, edge proxy, or service mesh. Whoever owns the deployment of this service almost certainly has one of those already.

### 11.10 Distributed tracing (OpenTelemetry / Zipkin)

**Rejected because:** MDC tracing covers the single-service case adequately, and OTel without an upstream propagating the span context is half a feature.

**What was on the table.** Instrument the service with OpenTelemetry, export spans to a collector.

**Why we didn't within scope.** OTel is most valuable when the trace spans *multiple services*. If this is the only service in the chain, the trace data is roughly equivalent to MDC plus timing — and MDC is already there. Adding the OTel runtime, the span context propagation, and the collector configuration buys nothing measurable until there's a second service to thread spans through.

**What would have to change to add it.** Add `io.opentelemetry:opentelemetry-spring-boot-starter`, configure an exporter (OTLP, Zipkin, Jaeger), and arrange for upstream services to propagate `traceparent` headers. The MDC layer is structured to be replaceable: an OTel-aware filter can populate MDC slots from the span context with no other code changes. This is a clean upgrade path, not a rewrite.

**The recommended replacement.** Once an upstream service exists that propagates span context, the upgrade is straightforward. Until then, MDC is enough.

### 11.11 Outbox pattern / event publishing

**Rejected because:** there is no consumer for transfer events in scope, and adding the pattern without a consumer is overengineering.

**What was on the table.** Inside the executing transaction, write a row to an `outbox_events` table alongside the transfer; a separate poller publishes to Kafka / RabbitMQ / whatever.

**Why we didn't.** The outbox pattern solves the dual-write problem ("how do I commit a database transaction *and* publish to a message broker atomically?"). We don't have a message broker, so we don't have the dual-write problem. Adding the outbox now would just give us a table that nobody consumes from.

**What would have to change to add it.** A migration for `outbox_events`, a poller process (or replication-log-based CDC), broker infrastructure, and a downstream consumer that has a real reason to need the events. The first three are small; the fourth is a project-defining decision (which events? what schema? at-least-once or exactly-once delivery?).

**The recommended replacement.** None within this service. The schema is already structured to make adding an outbox table later cheap — `transfers` has stable IDs and timestamps, the executing transaction is well-bounded, the outbox would slot in with one INSERT. The pattern is well-understood and documented enough that adding it on demand is a small project, not an architectural overhaul.

### 11.12 The boundary, summarized

What's *inside* this service: a single, atomic transfer with its idempotency, concurrency, and audit guarantees. What's *outside*: identity, authorization, currency policy, settlement, asynchronous workflows, rate limiting, multi-service tracing, event-driven integration, bulk operations.

The principle: this service does *one thing well*. Anything that wants to do *more* belongs to a layer above (gateway, orchestrator, settlement engine) or sits in front of (API contract, identity provider). Pinning that boundary is what lets the code stay small enough to be trustworthy at a glance.

---

## Glossary

Terms and abbreviations used in this document, with their precise meaning *as we use them here* — not the textbook definitions, since several have meaning that drifts in the wider literature.

| Term | Meaning here |
| --- | --- |
| **Advisory lock** | A Postgres cooperative lock keyed by a 64-bit integer, acquired via `pg_try_advisory_xact_lock` (transaction-scoped) or `pg_try_advisory_lock` (session-scoped). Used here as a fast in-flight reject for concurrent retries (§3) and as the leader-election primitive for the cleanup sweep (§10). |
| **Append-only** | A table that the database refuses to UPDATE or DELETE. Enforced by `BEFORE UPDATE/DELETE` triggers. `ledger_entries` is fully append-only; `wallets` is `DELETE`-blocked but row UPDATEs are allowed (the balance changes). |
| **Atomicity** | All-or-nothing semantics for a set of writes. Provided by Postgres transactions; `executeBalancedTransfer` writes five rows atomically (§5.2). |
| **Backoff with jitter** | A retry that sleeps for `base + uniform(0, jitter)` ms before the next attempt. Jitter prevents lockstep convergence under multi-caller retries (§6.4). |
| **Birthday bound** | Probabilistic collision rate for hash-derived identifiers. The 64-bit fold in `Hashing.toLockId` crosses 50% probability around 2³² simultaneous unique items (§3.4). |
| **Bounded retry** | A retry loop with a hard ceiling on attempts. Beyond the ceiling, surface 503 + Retry-After. Prevents transient incidents from becoming self-DoS (§6.2). |
| **Cached body** | The request body, copied into a `byte[]` at the first filter that needs to re-read it. Implemented by `CachedBodyHttpServletRequest` (§8.5). |
| **Compensating transfer** | A new transfer that reverses an earlier one — opposite direction, same amount, reference back to the original. The only legitimate way to "reverse" a posted transfer; direct ledger edits are blocked by trigger (§5.7, §11.8). |
| **Conflict-then-rollback race** | The hard idempotency race: R1 claims the key, then rolls back; R2 observes the claim, then re-reads and finds the row absent. Drives the `TransferService` retry loop (§3.3 Race D, §6.3). |
| **Constant-time comparison** | Byte-by-byte equality check whose runtime is independent of how many leading bytes match. Implemented by `MessageDigest.isEqual`; defeats timing-side-channel attacks on signature verification (§8.4). |
| **Covering index** | A B-tree index that includes additional columns via `INCLUDE (...)`, allowing index-only scans without heap fetches. Used on `transfers` for `findByWalletId` (§5.5). |
| **Deterministic lock order** | Acquiring locks in an order that is a function of the *resources*, not the *operation*. We sort wallet IDs lexicographically, so A→B and B→A both lock the lex-smaller wallet first; deadlock-free by construction (§4.4). |
| **Double-entry ledger** | Every transfer produces exactly two ledger rows (one DEBIT, one CREDIT) summing to zero. Enforced at schema level by `UNIQUE (transfer_id, wallet_id)` (§5.1). |
| **Hash conflict** | Same `(client_id, idempotency_key)` reused with a different request payload. Surfaces as `409` with a hash-conflict reason; distinct from the `409 In-Progress` shape (§3.3 Race B, §6.1). |
| **Hikari** | The connection pool used by Spring Boot by default. Returns connections from a fixed-size pool; consecutive statements from one Java method may land on different physical connections (§10.5). |
| **HMAC** | Hash-based Message Authentication Code; here, HMAC-SHA256 signing the string `method:path:clientId:idempotencyKey:timestamp:sha256(body)` (§8.3). |
| **Idempotency claim** | The `INSERT ... ON CONFLICT DO NOTHING` into `idempotency_records` that turns a `(client_id, key)` into a row. The atomic dedup primitive (§3.2). |
| **Idempotency record** | A row in `idempotency_records`. Lifecycle: `IN_PROGRESS → COMPLETED` (success) or `IN_PROGRESS → (rollback) → FAILED` (business failure recorded via REQUIRES_NEW) (§3.1). |
| **Index-only scan** | Postgres scan that reads only the index, no heap fetches. Achieved via covering indexes on `transfers` (§5.5). |
| **In-progress (409)** | Response to a duplicate request that arrived while a peer transaction is still executing the same key. Advisory-lock-fast-rejected to avoid blocking on the unique index (§3.3 Race A, §6.1). |
| **Leader election** | Selecting one replica from a multi-replica deployment to perform a singleton task. Implemented for the cleanup sweep via `pg_try_advisory_xact_lock` on a stable lock id (§10.4). |
| **Lex-min / lex-max** | Lexicographic minimum and maximum of a pair of strings. Used to compute deterministic wallet lock order (§4.2). |
| **Lock_timeout** | Postgres setting that bounds how long a statement waits on a row lock. Set to 8s via `connection-init-sql`. Trips with SQLState `55P03` (§9.1). |
| **MDC** | Mapped Diagnostic Context; SLF4J's per-thread map for structured log enrichment. Populated by `TracingFilter` with `requestId`, `correlationId`, `clientId` (§9.4). |
| **PROPAGATION_REQUIRES_NEW** | Spring transaction propagation mode that opens a brand-new transaction independent of any caller-side context. Used by `IdempotencyFailureRecorder` and `IdempotencyCleanupService` per-batch (§7.1, §10.6). |
| **Replay** | A duplicate request after the original committed. The cached `(response_status, response_body)` is shipped verbatim (§3.3 Race C). |
| **Replay window** | The lifetime during which a duplicate request gets the cached response. 72 hours by default; bounded by `idempotency.cleanup.retention-hours` (§10.1). HMAC has a separate replay window of 5 minutes bounded by `X-Timestamp` (§8.6). |
| **REQUIRES_NEW** | Shorthand for `PROPAGATION_REQUIRES_NEW`. |
| **Retention sweep** | The hourly cron that DELETEs idempotency records older than the retention threshold. Leader-elected, batched, bounded per tick (§10). |
| **Rolled-back IN_PROGRESS race** | See *conflict-then-rollback race*. |
| **Running balance** | The post-update wallet balance recorded on each ledger entry. Taken from the persisted row after `walletRepository.save`, not from the in-memory entity (§5.2). |
| **SQLState** | Postgres' machine-stable error code (5-character class+subclass). The classification primitive `GlobalExceptionHandler` uses for the 504 mapping (§6.6, §9.3). |
| **Statement_timeout** | Postgres setting that bounds how long a single statement runs. Set to 10s via `connection-init-sql`. Trips with SQLState `57014` (§9.1). |
| **Transient conflict** | The retry budget for the conflict-then-rollback race exhausted. Surfaces as `503 Service Unavailable + Retry-After` (§6.2). |
| **Two-tier lock** | The advisory lock + unique index pairing on the idempotency claim path. Advisory lock fast-rejects in-flight retries; unique index provides durable correctness (§3.2). |

---

## Appendix A — Where things live

A concept-to-code cross-reference. When the deep dive describes a behaviour, this table tells you where to find it.

| Concept | File | Notes |
| --- | --- | --- |
| Idempotency claim primitive | `IdempotencyRecordRepository.insertIdempotencyRecord` | `INSERT ... ON CONFLICT DO NOTHING` |
| Advisory lock fast-reject | `IdempotencyRecordRepository.tryAdvisoryXactLock` | `pg_try_advisory_xact_lock` |
| Bounded retry loop | `TransferService.executeTransfer` | maxAttempts, jittered backoff |
| Conflict resolution | `TransferService.resolveConflict` | replay vs hash-conflict vs absent |
| Executing transaction | `TransferExecutionService.tryExecuteTransfer` | `@Transactional(READ_COMMITTED)` |
| Wallet locking | `TransferExecutionService.lockBothWallets` | `findByIdForUpdate` in lex order |
| Atomic 5-row write | `TransferExecutionService.executeBalancedTransfer` | transfer + 2 wallets + 2 ledger |
| Failure recording | `IdempotencyFailureRecorder` | `REQUIRES_NEW`, INSERT-only |
| Sealed result type | `TransferService.TransferResult` | `Fresh` / `Replay` exhaustive switch (nested in `TransferService`) |
| Schema invariants | `db/migration/V1__init.sql` | CHECK, UNIQUE, FK, triggers |
| Idempotency cleanup index | `db/migration/V2__add_idempotency_cleanup_index.sql` | `idx_idempotency_first_seen_at` |
| Wallet append-only | `db/migration/V3__enforce_wallet_immutability.sql` | `BEFORE DELETE` trigger |
| Retention sweep | `IdempotencyCleanupService.cleanupExpiredRecords` | `@Scheduled` cron, batched |
| Leader election | `IdempotencyCleanupService.deleteOneBatch` | `pg_try_advisory_xact_lock` per batch |
| MDC tracing | `TracingFilter` | `HIGHEST_PRECEDENCE`, `MDC.clear` in `finally` |
| Idempotency-key gate | `IdempotencyHeaderFilter` | path normalization, 256-char cap |
| HMAC verification | `SignatureFilter` | HMAC-SHA256, 5-min TTL, constant-time compare |
| Body cache | `CachedBodyHttpServletRequest` | enables filter+controller body re-read |
| SQLState → 504 mapping | `GlobalExceptionHandler.handleCannotAcquireLock` (typed, 55P03), `handleResourceFailure` (SQLState walk: 57014, 55P03 fallback, classes 57P/08) | typed handler primary, SQLState walk defense-in-depth |
| Validation → 400 | `GlobalExceptionHandler.handleValidation` | sorted, deterministic message |
| Hashing primitives | `Hashing.sha256Hex`, `Hashing.toLockId` | request hash + 64-bit lock id |
| Log masking | `LogSafe.mask` | idempotency-key only |
| Metrics catalogue | `TransferMetrics` | 9 outcomes, 2 reasons, 5 meters |
| Configuration surface | `application.yml` | retry, retention, signature, timeout knobs |
| Hikari init sql | `application.yml` (`connection-init-sql`) | `lock_timeout=8s`, `statement_timeout=10s` |

---

## Appendix B — Reading paths

This document is long. Read by *concern*, not top-to-bottom.

**"I'm reviewing this for the assignment."** Start with `DESIGN.md` end-to-end (~15 minutes), then this document's §1 (Reading the assignment) for the prompt-clause-by-prompt-clause walkthrough. The rest of this document is reference material; dip in by section header when something in `DESIGN.md` raises a question.

**"I'm investigating a bug in production."** Start with §9.9 (Operational consequences for timeouts) and §9.6 (which metrics to alert on). Then §6.1 (the response-code matrix) to map symptom to mechanism. Then the relevant deep section: idempotency hazards in §3, lock contention in §4, retry behaviour in §6, failure recording in §7, leader election or eviction in §10.

**"I'm extending the service."** Read §11 first to understand what's deliberately *not* in scope. Then the relevant subsystem deep dive. The "Alternatives rejected" subsection in each section is the most useful read for "why didn't we do X?" before you re-propose X.

**"I'm onboarding to the codebase."** Read `DESIGN.md` for the model, then §2 of this document for the layer-by-layer mechanics, then Appendix A as a navigation index when you start clicking through the source.

**"I'm tightening security."** Read §8 (Authentication) end-to-end, then §11.1 (wallet ownership) and §11.9 (rate limiting) for the boundary calls, then the related operational consequences in §9.9.

**"I'm scaling this service."** Read §10 (lifecycle) and the partitioning notes in §10.9, plus the Hikari + timeout discussion in §9.1–§9.2. The retention-sweep math in §10.7 is the operational lever.

**"Something feels overengineered."** §11 and the "Alternatives rejected" subsections are the inventory of what we ruled out. If the thing that feels overengineered isn't there, the answer is in the relevant subsystem section — usually with a specific failure mode pinned to a concrete race.
