# Technical Design Document — Wallet Transfer Service

> Documentation-first design note. This precedes/accompanies implementation per the
> assignment's Documentation-First Workflow. It is the source of truth for the contract and
> the design decisions; code and tests follow from it.

## Status & Scope

| | |
|---|---|
| **Stack** | Java 21, Spring Boot 3.3.4, Spring Data JPA / Hibernate 6.5, PostgreSQL, Flyway |
| **In scope** | `POST /transfers` with exactly-once semantics, double-entry ledger, balance tracking, safe concurrency; read APIs for wallet balance and transfer history |
| **Out of scope (optional)** | metrics dashboards, async workflows |
| **Built** | Schema (Flyway `V1`, `V2`, `V3`), domain entities (`Wallet`, `Transfer`, `LedgerEntry`, `IdempotencyRecord`), base classes (`BaseEntity`, `AuditableEntity`), repository / service / controller layers (transfer + wallet read APIs), exception handling, and the full test suite (domain unit + Testcontainers integration, idempotency, concurrency) |

**Preferred order of work** (complete): ✅ inspect contract (`ASSIGNMENT.md`) → ✅ design note (this doc) → ✅ implement code → ✅ add tests → ✅ verify observability/operational concerns.

---

## 1. Problem Statement

Build a service supporting wallet-to-wallet transfers that is **correct under retries, duplicates, and concurrent access**. A single endpoint, `POST /transfers`, moves an amount from one wallet to another. The hard requirements are reliability properties, not features:

- **Exactly-once at the API level** when an `idempotencyKey` is supplied — a retried request must never move money twice and must return the original result.
- **Double-entry ledger** — every transfer produces exactly one DEBIT and one CREDIT; the ledger always balances.
- **Correct balances under concurrency** — no double-spend when two transfers debit the same wallet simultaneously.
- **Safe state transitions** — a transfer moves through a guarded state machine.

---

## 2. Expected Behavior

A transfer is processed **synchronously inside a single database transaction**. By the time the client gets a response, the outcome is final (`PROCESSED` or `FAILED`); `PENDING` is a transient in-transaction state, never observed as a stable result by the caller.

Happy path:

```
POST /transfers
  BEGIN TX
    reserve idempotency key (INSERT IN_PROGRESS)
    SELECT both wallets FOR UPDATE   (deterministic lock order)
    create transfer (PENDING)
    debit source, credit destination
    write 2 ledger entries (DEBIT, CREDIT)
    mark transfer PROCESSED
    complete idempotency record (cache 201 response + target_id)
  COMMIT
-> 201 Created { transfer in PROCESSED state }
```

Business failure (e.g. insufficient funds) is a **first-class, recorded outcome**, not an exception that vanishes: the transfer is persisted as `FAILED` with a reason, `422` is returned, and that response is cached so a retry replays it.

---

## 3. API Contract

### `POST /transfers`

Request body:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "018f...uuid",
  "toWalletId":   "018f...uuid",
  "amount": 100.00
}
```

| Field | Type | Rules |
|---|---|---|
| `idempotencyKey` | string | required, non-blank; client-generated, unique per logical request |
| `fromWalletId` | UUID | required, must exist |
| `toWalletId` | UUID | required, must exist, `!= fromWalletId` |
| `amount` | decimal | required, `> 0`, scale ≤ 2 |

> Note: wallet ids are **UUIDs** (the assignment's `wallet_1` examples are illustrative). All ids are server-assigned UUID **v7** (time-ordered).

Success / outcome response body:

```json
{
  "transferId": "018f...uuid",
  "fromWalletId": "018f...uuid",
  "toWalletId": "018f...uuid",
  "amount": 100.00,
  "status": "PROCESSED",
  "failureReason": null,
  "createdAt": "2026-06-18T10:00:00Z"
}
```

Response codes:

| Code | Meaning |
|---|---|
| `201 Created` | transfer executed (`PROCESSED`) |
| `422 Unprocessable Entity` | business failure recorded as `FAILED` (e.g. insufficient funds, currency mismatch) — body carries `status: FAILED` + `failureReason` |
| `400 Bad Request` | validation error (missing field, `amount <= 0`, same wallet) |
| `404 Not Found` | wallet does not exist |
| `409 Conflict` | idempotency key reused with a **different** payload |
| **replay** | a duplicate of the **same** request — arriving after completion *or* concurrently — returns the **original** status code and body verbatim. A concurrent duplicate blocks on the unique index until the winner commits, then replays it (it does **not** get a fast in-flight `409`) |

### `GET /wallets/{id}` — balance

Returns the wallet's current balance. `200 OK` with `{ walletId, balance, currency, updatedAt }`; `404` if the wallet does not exist; `400` if the id is not a valid UUID.

```json
{ "walletId": "018f...", "balance": 250.75, "currency": "INR", "updatedAt": "2026-06-20T10:00:00Z" }
```

### `GET /wallets/{id}/transfers` — transfer history (paginated)

Returns a **paginated** page of the transfers the wallet participated in (as source or destination), **newest first**. Accepts the standard Spring Data paging params (`page`, `size`, `sort`; defaults `page=0`, `size=20`, `sort=createdAt,id,desc`). Each item is the same `TransferResponse` shape used by `POST /transfers`, wrapped in a stable `PageResponse` envelope (`content`, `page`, `size`, `totalElements`, `totalPages`, `first`, `last`). `200 OK` with an empty `content` when there is no history; `404` if the wallet does not exist; `400` if the id is not a valid UUID.

```json
{ "content": [ /* TransferResponse… */ ], "page": 0, "size": 20, "totalElements": 1, "totalPages": 1, "first": true, "last": true }
```

Both reads run in a `readOnly` transaction and are served by primary-key / FK indexes (no scans). The explicit `PageResponse` is used instead of Spring Data's `Page` to keep the JSON contract version-stable.

### Optional (not built)

`GET /transfers/{id}` (single transfer) and `GET /wallets/{id}/ledger` (raw ledger entries) — natural extensions, omitted as the balance + history reads already cover the optional scope.

---

## 4. Data Model & Side Effects

All entities extend `BaseEntity` (UUID **v7** primary key, assigned by Hibernate's `UuidGenerator.Style.TIME` on persist — time-ordered keys keep PK-index inserts append-friendly) and `AuditableEntity` (`created_at` / `updated_at` managed by Spring Data JPA auditing).

| Table | Purpose | Key columns / constraints |
|---|---|---|
| `wallets` | materialized balance | `balance NUMERIC(19,2)` , `currency`, `CHECK balance >= 0` |
| `transfers` | request + lifecycle | `from_wallet_id`, `to_wallet_id` (FK→wallets), `amount`, `status`, `failure_reason`; `CHECK amount > 0`, `CHECK from <> to`, `CHECK status IN (...)` |
| `ledger_entries` | append-only double-entry | `wallet_id`, `transfer_id` (FKs), `type` (DEBIT/CREDIT), `amount`, `balance_after`; immutable |
| `idempotency_records` | generic exactly-once registry | `idempotency_key UNIQUE`, `request_hash`, `status`, `target_id`, `response_status`, `response_body` |

**Balance strategy:** materialized `balance` column updated in the same transaction as the ledger entries (chosen over deriving from the ledger) for O(1) reads. The invariant `balance == SUM(credits) − SUM(debits)` holds for every committed transaction; `ledger_entries.balance_after` snapshots the balance per entry for auditability.

**Side effects of a successful `POST /transfers`** (all within one transaction, all-or-nothing):
1. one `idempotency_records` row (`COMPLETED`, with cached response + `target_id`),
2. one `transfers` row (`PROCESSED` or `FAILED`),
3. two `ledger_entries` rows (DEBIT + CREDIT),
4. updated `balance` on one or both wallets.

Migrations: `V1__init.sql` (wallets, transfers, ledger_entries), `V2__create_idempotency_records.sql`, and `V3__amount_precision_two_decimals.sql` (narrows monetary columns from `NUMERIC(19,4)` to `NUMERIC(19,2)` to match the scale ≤ 2 contract). Flyway runs on app startup before Hibernate `validate`.

---

## 5. Failure Modes

| Failure | Detection | Outcome |
|---|---|---|
| Insufficient funds | balance check under lock | transfer `FAILED` + `422`, cached |
| Wallet not found | lookup returns empty | `404`, no transfer row |
| Same source & destination | bean validation + DB `CHECK` | `400` |
| Non-positive / malformed amount | bean validation + DB `CHECK` | `400` |
| Currency mismatch (assumption: no FX) | compare wallet currencies | transfer `FAILED` + `422` |
| Concurrent debit of same wallet | `SELECT … FOR UPDATE` serializes | no double-spend; second waits then re-checks funds |
| Duplicate request (same key, same payload, completed) | unique key / lookup | replay cached response |
| Concurrent identical duplicate (still in flight) | blocks on the unique index until the winner commits | replays the winner's response (a committed `IN_PROGRESS` is never visible under READ COMMITTED, so there is no fast in-flight `409`) |
| Key reused with different payload | `request_hash` mismatch | `409` |
| Unique-violation race (two firsts insert same key) | `idempotency_records_key_unique` constraint | loser caught, treated as duplicate (replay) |
| Other integrity violation (FK / CHECK / NOT NULL) | constraint name ≠ idempotency-key | rethrown, logged → `500` (never misread as a duplicate `409`) |
| Process crash mid-transaction | transaction never commits | full rollback; no partial money movement |

**Defense in depth:** invariants are enforced both in the domain (`Wallet.debit` throws if it would go negative; `Transfer.markProcessed/markFailed` only allow transitions out of `PENDING`; `IdempotencyRecord.complete` is one-way `IN_PROGRESS → COMPLETED`, so a cached response can never be overwritten) **and** at the database (`CHECK`/`UNIQUE`/`FK` constraints), so a logic bug cannot corrupt persisted state. Entities expose **getters only** (no `@Setter`), so the only way to mutate state is through these guarded domain methods — there is no setter that could bypass them (JPA uses field access, so setters are unnecessary).

---

## 6. Idempotency Behavior

Idempotency is handled by a **dedicated, operation-agnostic `idempotency_records` table** rather than a unique constraint on `transfers`, so the same mechanism can guard future endpoints and can **replay a cached response**.

Record shape: `idempotency_key` (unique), `request_hash` (a SHA-256 fingerprint of the canonical request fields — `fromWalletId|toWalletId|amount`, with the amount's trailing zeros stripped so `100` and `100.00` hash alike), `status` (`IN_PROGRESS` → `COMPLETED`), `target_id` (created resource id), `response_status` + `response_body` (cached response).

Algorithm (inside the transfer transaction):

1. Compute `request_hash`. `INSERT` an `IN_PROGRESS` record keyed by `idempotencyKey`.
2. **Insert succeeds** → first occurrence → execute the transfer, then `complete(targetId, status, body)` to flip the record to `COMPLETED` and cache the response.
3. **Insert hits the unique violation** → duplicate. The loser **blocks on the unique index** until the winner's transaction commits, then loads the (now `COMPLETED`) record:
   - matching `request_hash` → **replay** cached `response_status`/`response_body`.
   - `request_hash` mismatch → `409` (key reused for a different request).

> Because reservation and completion happen in **one** transaction, a committed record is always already `COMPLETED` — under READ COMMITTED another request can never observe a committed `IN_PROGRESS`. So a concurrent duplicate always ends in a replay, never a fast in-flight `409`. The `IN_PROGRESS → 409` check in code is a deliberate **defensive guard** that only becomes reachable if the reservation is later moved into its own committed transaction (e.g. a `REQUIRES_NEW` reservation).

This gives **exactly-once side effects** (duplicate never produces a second transfer/ledger pair) and **return-the-original-result** semantics, both safe across process restarts because the registry is durable.

---

## 7. Retry Behavior

- **Client-driven retries are safe**: re-sending with the same `idempotencyKey` either replays the cached result or is rejected as in-flight — never double-applied.
- **Network failure after commit but before the client sees the response**: the retry finds a `COMPLETED` record and replays the original `201`/`422`.
- **Failure before commit**: nothing persisted (atomic rollback); the retry is treated as first occurrence.
- **Stale `IN_PROGRESS` records** (writer crashed after reserving the key but before commit): the reserving `INSERT` is part of the same transaction, so a crash rolls it back too — no orphan is committed. As an operational safeguard, a TTL/reaper for any `IN_PROGRESS` older than a threshold is noted (not required for correctness here).
- Operations are designed to be **retry-safe rather than relying on retries**; there is no internal auto-retry of business logic.

---

## 8. Consistency Expectations

- **Atomicity**: each transfer is one ACID transaction — idempotency record, transfer row, both ledger entries, and balance updates commit together or not at all.
- **Isolation**: `READ COMMITTED` (Postgres default) + **row-level pessimistic locks** (`SELECT … FOR UPDATE`) on the participating wallets. Because we lock the exact rows we mutate, this is sufficient; no `SERIALIZABLE` needed.
- **Deadlock avoidance**: wallets are always locked in a **deterministic order** (e.g. ascending wallet id), so two opposing transfers between the same pair cannot deadlock.
- **Concurrency strategy = pessimistic locking only.** (Optimistic `@Version` was intentionally removed; with row locks held for the wallet's mutation window, the lost-update window is closed without it.)
- **Ledger invariant**: every transfer nets to zero across wallets; `SUM(credits) − SUM(debits)` per wallet always equals its `balance`.

---

## 9. Observability Expectations

- **Structured logging** keyed by `idempotencyKey` and `transferId` for the lifecycle: request received → key reserved / duplicate-replayed → transfer PROCESSED/FAILED → committed. Log level for `com.rajat.wallet` is `INFO`.
- **Metrics** (when added): transfer count by terminal status, transfer latency, lock-wait time, duplicate-replay rate, insufficient-funds rate.
- **Health/readiness** (when added): Spring Actuator health/readiness probes. Today, Flyway migration state is visible at startup (`Successfully applied N migrations`).
- **Auditability**: append-only `ledger_entries` + `balance_after` snapshots + `created_at`/`updated_at` on every row provide a full reconstructable history.
- Tracing (correlation id propagation) is a nice-to-have, not required.

---

## 10. Testing Strategy

Behavioral, TDD (Red → Blue → Green). PostgreSQL-backed integration tests via **Testcontainers** (no in-memory substitute, so locking/constraints are exercised for real).

- **Domain unit tests**: `Wallet.debit` rejects overdraft; `credit`/`debit` arithmetic; `Transfer` state machine rejects illegal transitions; `IdempotencyRecord` lifecycle.
- **Transfer execution (integration)**: happy path returns `201` `PROCESSED`; exactly two ledger entries (one DEBIT, one CREDIT); balances move correctly; ledger balances.
- **Idempotency (integration)**: same key + same payload → single transfer, replayed response; same key + different payload → `409`; verifies **no duplicate side effects**.
- **Failure scenarios**: insufficient funds → `FAILED` + `422`, and a retry replays the `422`; unknown wallet → `404`; same-wallet / bad amount → `400`.
- **Concurrency**: N parallel transfers debiting one wallet → no overdraft, final balance exact, no lost updates; concurrent duplicates of the same key → exactly one transfer created.
- **Read APIs (integration)**: `GET /wallets/{id}` returns the balance and reflects it after a transfer; `GET /wallets/{id}/transfers` returns every transfer involving the wallet, newest first, and paginates correctly (`page`/`size` slice + `totalElements`/`totalPages`/`first`/`last`); unknown wallet → `404`; malformed id → `400`.
- **Error mapping (unit)**: the idempotency-key unique violation maps to `409` while any other integrity violation maps to a logged `500`; the orchestrator replays only on the idempotency-key violation and rethrows everything else.

Coverage targets the **required behaviors** (transfer execution, idempotency, ledger correctness, failure handling, concurrency safety), not implementation details.

---

## Assumptions

- Transfers are **single-currency** (no FX); a currency mismatch is a recorded `FAILED` outcome.
- `idempotencyKey` is carried in the **request body** (per the assignment example); a header (`Idempotency-Key`) is an equivalent alternative.
- No authentication/authorization layer (out of scope for the exercise).
