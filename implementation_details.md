# Implementation Details

Deep-dive into the architecture, the design decisions behind it, the trade-offs each one carries, and the performance characteristics. This complements the [`README.md`](./README.md) (usage) and the [`TechnicalDesignDocument.md`](./TechnicalDesignDocument.md) (the documentation-first contract).

---

## 1. Detailed Architecture

### 1.1 Request lifecycle

```
POST /transfers
  │
  ▼
TransferController            validate body (bean validation) → delegate → map outcome to HTTP status
  │
  ▼
TransferServiceImpl          NOT @Transactional — the orchestrator
  │  1. compute request_hash
  │  2. fast path: findByIdempotencyKey → if present, replay or 409
  │  3. else delegate to the processor
  │  4. on idempotency-key unique violation: re-read winner and replay (other violations rethrown)
  ▼
TransferProcessor.process()  @Transactional — ONE atomic unit of work
  │  1. saveAndFlush(IdempotencyRecord.inProgress)   ← reserves the key, collides early
  │  2. lock both wallets  SELECT … FOR UPDATE ORDER BY id
  │  3. save Transfer(PENDING)                       ← gets a UUID v7 id for the ledger FKs
  │  4. validate under lock (currency, funds)
  │  5a. fail → transfer.markFailed(reason)          ← still COMMITS (recorded outcome)
  │  5b. ok → debit/credit wallets + 2 ledger entries + transfer.markProcessed
  │  6. record.complete(targetId, status, body)      ← cache the response
  ▼
COMMIT
```

### 1.2 The service / processor split (the central decision)

The two beans exist so the **transaction boundary is in the right place**:

- `TransferProcessor` is a **separate Spring bean** annotated `@Transactional`. Because Spring transactions are proxy-based, the boundary is only real when the call **crosses a bean boundary** — an inner method annotated `@Transactional` on the same class would be ignored. Putting it on its own bean makes the boundary genuine.
- `TransferServiceImpl` is **deliberately not transactional**. It must be able to observe the *committed* result of a competing transaction. If it shared a transaction with the processor, a `DataIntegrityViolationException` would mark that transaction rollback-only ("poisoned"), and it could not then turn around and read the winner.

This split is what turns a concurrent duplicate from an error into a **transparent replay**. See §2.2.

### 1.3 Entity model

All entities extend `BaseEntity` (UUID v7 id) → `AuditableEntity` (`created_at` / `updated_at`), both `@MappedSuperclass`. Domain behavior lives on the entities:

- `Wallet.debit()` throws if it would go negative — a last line of defence independent of the service.
- `Transfer.markProcessed()` / `markFailed()` call a private `requirePending()` guard, so the state machine only ever moves *out of* `PENDING` once.
- `LedgerEntry` is immutable (getters only, `@AllArgsConstructor`) — it models an append-only fact.

**No public setters.** Entities expose `@Getter` but deliberately *not* `@Setter`. The only ways to mutate state are the intention-revealing domain methods (`debit`/`credit`, `markProcessed`/`markFailed`, `inProgress`/`complete`) — there is no `setBalance` or `setStatus` escape hatch that could bypass the overdraft guard or the `requirePending()` state-machine check. This works because JPA uses **field access** here (the `@Id` annotation sits on the field in `BaseEntity`), so Hibernate hydrates via reflection and never needs setters; the invariants stay centralized in the aggregate.

### 1.4 Read side (query APIs)

Two read-only endpoints sit alongside the write path, served by a separate `WalletService` / `WalletController` so the command and query sides stay cleanly separated:

- **`GET /wallets/{id}`** — returns the **materialized** balance directly (`findById`), so a balance read is O(1) and never aggregates the ledger. The `updatedAt` audit column doubles as a freshness signal (timestamp of the last balance-changing transaction).
- **`GET /wallets/{id}/transfers`** — **paginated** transfer history for a wallet (source *or* destination) via `findByWalletId(walletId, Pageable)`. Paging/sorting come from a `Pageable` (default `size=20`, `sort=createdAt,id desc`); the `id` tiebreaker — a time-ordered UUID v7 — keeps paging deterministic when `createdAt` values collide. To avoid a redundant round-trip, it queries the page **first** and only runs `existsById` when the page is empty — so a wallet with history skips the existence check entirely, while an empty result still distinguishes "no transfers yet" (`200`, empty) from "wallet missing" (`404`). The result is mapped to an explicit `PageResponse` envelope rather than returning Spring Data's `Page` directly, whose JSON shape is version-unstable.

Both run in a `@Transactional(readOnly = true)` boundary, mapping entities to DTOs while the session is open (`open-in-view` is disabled). A malformed UUID in the path is mapped to `400` (`MethodArgumentTypeMismatchException`) rather than leaking a `500`.

---

## 2. Design Decisions & Trade-offs

### 2.1 Materialized balance vs. derived-from-ledger

**Decision:** keep a materialized `balance` column on `wallets`, updated in the same transaction as the ledger entries.

| | Materialized (chosen) | Derived (`SUM` of ledger) |
|---|---|---|
| Read cost | **O(1)** | O(n) aggregate per read |
| Write cost | one extra UPDATE | none |
| Correctness | invariant must be maintained in-tx | always correct by construction |

**Trade-off:** we accept the responsibility of keeping `balance == SUM(credits) − SUM(debits)` consistent (done by always writing both inside one transaction, under the wallet lock) in exchange for cheap reads. `ledger_entries.balance_after` snapshots the balance per entry, so the materialized value is always auditable/reconstructable against the ledger.

Every monetary column is `NUMERIC(19,2)` in Flyway **and** mapped explicitly on the entity (`@Column(precision = 19, scale = 2)` on `wallets.balance`, `transfers.amount`, `ledger_entries.amount` / `balance_after`). Pinning precision/scale in code keeps `ddl-auto: validate` robust across Hibernate/dialect versions (it doesn't fall back to a dialect default) and documents the intended money scale right next to the field — consistent with the `@Digits(fraction = 2)` request validation.

### 2.2 Pessimistic locking vs. optimistic vs. serializable

**Decision:** pessimistic row locks (`SELECT … FOR UPDATE`) under `READ COMMITTED`.

- **vs. optimistic (`@Version`)** — optimistic locking surfaces conflicts as retryable failures *after* the work is done; under contention on a hot wallet that means wasted work and client-visible retries. With pessimistic locks the contending transaction simply waits, then proceeds correctly. (`@Version` was intentionally removed.)
- **vs. `SERIALIZABLE` isolation** — would also be correct, but it pushes conflict handling to serialization-failure retries across the whole transaction and costs more. Because we lock *exactly the rows we mutate*, `READ COMMITTED` + row locks is sufficient.

**Deadlock avoidance:** wallets are always locked in **ascending id order** (`ORDER BY w.id` in the lock query), so two opposing transfers (A→B and B→A) acquire the same locks in the same order and cannot deadlock.

**Trade-off:** pessimistic locks serialize transfers that touch a shared wallet (lower parallelism on a hot account) in exchange for simplicity and zero lost-update risk. For a wallet system, correctness on the hot row is worth more than parallelism on it.

### 2.3 Dedicated idempotency table vs. unique constraint on `transfers`

**Decision:** a generic `idempotency_records` table, not a `UNIQUE(idempotency_key)` on `transfers`.

Reasons:
1. **Replayable response.** A unique constraint on `transfers` would *reject* a duplicate; it can't *return the original result*. The registry caches `response_status` + `response_body` and replays them verbatim.
2. **Operation-agnostic.** The same mechanism can guard any future endpoint; `target_id` is a plain UUID (not an FK) precisely so the registry is polymorphic across resource types.
3. **In-flight detection.** The `IN_PROGRESS → COMPLETED` lifecycle lets a concurrent duplicate distinguish "still running" from "done".

**Trade-off:** one extra row and one extra write per request, plus a `request_hash` to compute — in exchange for true exactly-once *with* response replay.

**Scoped violation handling.** Only the `idempotency_records_key_unique` violation is treated as the replayable race — `DataIntegrityViolations.isIdempotencyKeyViolation(...)` classifies it (by Hibernate constraint name, falling back to the SQL message). The orchestrator rethrows any *other* `DataIntegrityViolationException` (FK / CHECK / NOT NULL) instead of mistaking it for a duplicate, and the global handler logs it and returns `500` rather than a misleading retryable `409`. The cached `response_body` is mapped to the schema's `TEXT` column (`columnDefinition = "text"`) since a serialized response easily exceeds a default `varchar`.

### 2.4 `saveAndFlush` + unique index as the concurrency primitive

**Decision:** reserve the key with `saveAndFlush` at the very start of the transaction.

The `flush` forces the `INSERT` to the database immediately, *before* any money moves. Combined with `UNIQUE (idempotency_key)`, this means a concurrent duplicate **blocks on the index** at its own insert and never reaches the money-moving code. When the winner commits, the loser fails with a unique violation that the orchestrator converts to a replay. The reservation is part of the same transaction, so if the transfer rolls back, the key is released too — **no orphaned `IN_PROGRESS` rows** from a crash mid-flight.

**Trade-off:** a duplicate that arrives *during* the winner's transaction waits for it to commit (latency = winner's remaining work) instead of failing fast. That's the correct behavior for exactly-once.

A consequence worth calling out: because reservation and completion are in the **same** transaction, a committed record is always already `COMPLETED`, so under READ COMMITTED no other request can ever observe a committed `IN_PROGRESS`. Concurrent duplicates therefore always end in a **replay**, never a fast in-flight `409`. The `IN_PROGRESS → 409` branch in `replayOrConflict` is a deliberate defensive guard that only becomes reachable if the reservation is moved into its own committed transaction (`REQUIRES_NEW`). The contract and tests reflect the blocking-then-replay behavior.

### 2.5 Business failure as a recorded outcome (`422`), not an exception

**Decision:** insufficient funds / currency mismatch produce a persisted `FAILED` transfer and a `422`, and the transaction **commits**.

This makes failures **idempotent too**: the cached `422` is replayed on retry, so a client that retries a doomed transfer keeps getting the same answer rather than re-running validation. Only genuinely exceptional conditions (unknown wallet, serialization error) throw and roll back.

**Trade-off:** we store rows for failed attempts (useful for audit/observability) instead of discarding them.

### 2.6 UUID v7 primary keys

**Decision:** time-ordered UUID v7 (Hibernate `UuidGenerator.Style.TIME`), assigned by the app on persist.

- vs. random UUID v4: v7 is **time-ordered**, so primary-key index inserts are append-friendly (less B-tree fragmentation, better cache locality).
- vs. DB sequence/`BIGINT`: app-assigned ids are known before flush (the `Transfer` id is needed for the ledger FKs in the same unit of work) and avoid a round-trip; they're also non-guessable and merge-friendly across systems.

**Trade-off:** 16 bytes vs. 8 for a bigint, and slightly larger than v4 ordering benefits on some workloads — acceptable for the guarantees gained.

### 2.7 Synchronous, single-transaction processing

**Decision:** the whole transfer is one synchronous ACID transaction; by the time the client gets a response the outcome is final (`PROCESSED`/`FAILED`). `PENDING` is only a transient in-transaction state.

**Trade-off:** simplicity and strong consistency over throughput. An async/outbox/saga design would scale writes further but adds eventual-consistency complexity that this exercise's correctness goals don't need. The state machine (`PENDING`) is in place, so an async evolution is possible later.

---

## 3. Performance Considerations

### 3.1 Read/write costs per transfer

A successful transfer is a bounded, constant number of statements: reserve key (1 insert + flush), lock 2 wallets (1 select-for-update), insert transfer (1), update 2 wallets, insert 2 ledger entries, update the idempotency record (1). No N+1, no per-request scans — every access is by primary key or the unique idempotency index.

### 3.2 Indexing

- PK indexes (UUID v7) back every entity lookup.
- `UNIQUE (idempotency_key)` backs both the duplicate check and the reservation collision.
- FK indexes on `transfers(from_wallet_id, to_wallet_id)` and `ledger_entries(wallet_id, transfer_id)` keep history/audit queries from sequential-scanning.

### 3.3 Lock contention & throughput

Throughput is bounded by contention on a **single hot wallet**, since transfers touching it serialize on the row lock. Transfers on disjoint wallet sets run fully in parallel. The lock is held only for the wallet-mutation window of a short transaction, keeping the critical section small. If a single wallet ever became a throughput bottleneck, options (not implemented, not needed here) include balance sharding / striped sub-accounts or an async ledger with periodic balance projection.

### 3.4 Connection & transaction footprint

The orchestrator is non-transactional, so it holds **no** DB connection while deciding the fast path; a connection is taken only for the processor's transaction (and briefly for the initial idempotency lookup). The transaction is deliberately short — no external/network calls inside it — so connections are returned to the pool quickly.

### 3.5 Time-ordered keys

UUID v7 keeps PK-index inserts near-sequential, avoiding the write amplification and page splits that random v4 keys cause on high-insert tables (`transfers`, `ledger_entries`).

---

## 4. Failure Handling Summary

| Failure | Mechanism | Result |
|---|---|---|
| Insufficient funds / currency mismatch | check under lock | `FAILED` + `422`, cached & replayable |
| Unknown wallet | lookup empty → `WalletNotFoundException` | `404`, transaction rolled back |
| Concurrent debit, same wallet | `SELECT … FOR UPDATE` | second waits, re-checks funds — no double-spend |
| Concurrent duplicate key | `saveAndFlush` + unique index | loser blocks → unique violation → replay winner |
| Same key, different payload | `request_hash` mismatch | `409` |
| Other integrity violation (FK/CHECK/NOT NULL) | constraint name ≠ `idempotency_records_key_unique` | rethrown, logged → `500` (never misread as a duplicate) |
| Crash mid-transaction | atomic rollback | no partial state, no orphaned reservation |

Invariants are enforced **in the domain** (`Wallet.debit`, `Transfer` guards) **and at the database** (`CHECK`/`UNIQUE`/`FK`), so a logic bug cannot corrupt persisted state.

---

## 5. Observability

- Structured `INFO` logging keyed by `idempotencyKey` / `transferId` across the lifecycle (reserved → processed/failed → replayed).
- Append-only `ledger_entries` + per-entry `balance_after` + `created_at`/`updated_at` on every row give a fully reconstructable history.
- Flyway prints applied-migration state at startup.
- Natural metric hooks (not wired): transfer count by terminal status, latency, lock-wait time, duplicate-replay rate, insufficient-funds rate.

---

## 6. Future Scope: Asynchronous Processing

The current design is **synchronous** — a transfer is one ACID transaction and the client gets the final outcome (`PROCESSED`/`FAILED`) in the response. That choice favors immediate consistency, which is usually what a wallet user wants ("did it go through? yes, right now"). For higher throughput, spiky load, or multi-step transfers (external rails, compliance checks), the service can evolve to an **event-driven, asynchronous** model. The existing `PENDING → PROCESSED/FAILED` state machine, the idempotency registry, and the self-contained `TransferProcessor` are deliberate seams that make this evolution natural.

### 6.1 Target flow

```
Client ─POST /transfers─▶ API (accept)              → 202 Accepted { transferId, status: PENDING, statusUrl }
                            │ persist PENDING transfer + idempotency(IN_PROGRESS) + outbox row   (ONE tx)
                            ▼
                          Kafka  (topic: transfers, partitioned by wallet id)
                            ▼
                          Transfer consumer  → runs today's TransferProcessor logic
                            │ debit/credit + 2 ledger entries + mark PROCESSED/FAILED
                            ▼
                          Kafka  (topic: transfer-completed)
                            ├─▶ Notification service → email / push   (idempotent)
                            └─▶ SSE / WebSocket gateway → app
```

The API response becomes **`202 Accepted`** instead of `201`, and `PENDING` — invisible to the client today — becomes a first-class, observable state. A **`GET /transfers/{id}`** status endpoint becomes mandatory as the reliable source of truth (SSE and email are best-effort).

### 6.2 Correctness concerns (these matter more than the happy path)

1. **Dual-write → transactional outbox.** A service cannot atomically write the `PENDING` transfer to Postgres *and* publish to Kafka; a crash between the two loses or double-publishes the event. Write an **outbox row in the same DB transaction**, and let a relay (Debezium CDC or a poller) publish it. The event is published **iff** the transfer was persisted.

2. **At-least-once delivery → app-level idempotency.** Kafka redelivers (rebalances, retries), and its "exactly-once" only covers Kafka→Kafka — the side effect still lands in Postgres. The consumer must dedup on `idempotencyKey`/`transferId` and replay instead of re-applying. This makes the existing `idempotency_records` table **more** important, not less.

3. **Ordering vs. the DB lock.** Partition by wallet id for per-wallet ordering and predictable contention, but a transfer touches *two* wallets, so partitioning by one does not serialize the other. The `SELECT … FOR UPDATE` row locks remain the source of truth for no-double-spend; Kafka partitioning is a throughput/ordering optimization on top, not a replacement.

4. **Business failure ≠ poison message.** Insufficient funds is a valid *terminal* outcome (commit `FAILED`, notify) and must **not** be retried or dead-lettered. Only infrastructure failures (DB down, deserialization error) should retry / route to a DLQ.

5. **Notifications are also at-least-once.** Emit a `transfer-completed` event and let a separate notification service send email/push — never call external providers inline in the transfer consumer (it would couple the ledger commit to an email gateway). That service must also dedup on `transferId`, or users get duplicate notifications.

6. **SSE fan-out across instances.** SSE is per-connection and per-instance: the completion event must reach the instance holding *that* client's stream (each gateway subscribes to the topic and filters by user, or routes via Redis pub/sub). Always keep `GET /transfers/{id}` as the fallback, since SSE connections drop.

### 6.3 Trade-offs

| | Synchronous (current) | Asynchronous (future) |
|---|---|---|
| Consistency | immediate — outcome in the response | eventual — client handles `PENDING` |
| Throughput | bounded by request latency | Kafka buffers bursts; consumers scale out |
| Moving parts | one transaction | Kafka, outbox relay, notification svc, SSE fan-out, DLQ |
| Testability | one ACID tx, easy | distributed, harder to reason about |
| Best for | correctness-first wallet UX | high/spiky load, multi-step transfers |

**Recommendation:** don't move to async for *correctness* reasons (the synchronous model is stronger there) — only for *scale/throughput*. A pragmatic first step is to introduce the **outbox + Kafka path for the notification / downstream side effects** (which genuinely benefit from decoupling) while keeping the **ledger write itself synchronous**, then make the ledger async only if write volume demands it.
