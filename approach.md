# Wallet Transfer Service — Design Note

- Status: **Implemented**
- Author: Darshan Mehta

This document captures the design that was implemented for the
`POST /transfers` service. The narrative tracks the assignment's evaluation
criteria: schema, transaction strategy, idempotency, concurrency safety, and
testing.

---

## 1. Problem Statement

Build a service exposing `POST /transfers` that moves an amount between two
wallets with the following requirements:

- **Idempotent.** A second request with the same `idempotencyKey` returns the
  original response without re-executing side effects.
- **Double-entry ledger.** Every successful transfer produces exactly one
  DEBIT on the source wallet and one CREDIT on the destination wallet.
- **Transfer state machine.** Transfers go `PENDING → PROCESSED` or
  `PENDING → FAILED`; transitions are safe under retries.
- **Concurrency-safe.** Concurrent debits cannot double-spend; concurrent
  reverse-direction transfers cannot deadlock.
- **Cleanly layered.** Controller / service / repository / entity.

---

## 2. Stack

- **Java 17**, **Spring Boot 3.3** (web + data-jpa + validation)
- **PostgreSQL 16**, **Flyway** migrations
- **Hibernate** (JPA provider)
- **JUnit 5**, **AssertJ**, **Testcontainers Postgres** for integration tests
- **Gradle 8.10** (Kotlin DSL)

Amounts are represented as `BIGINT` minor units (paise/cents). Single currency,
no FX. Authentication and multi-tenancy are out of scope.

---

## 3. API Contract

**Endpoint** — `POST /transfers`

**Request body**

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet-a",
  "toWalletId":   "wallet-b",
  "amount": 100
}
```

Bean Validation:

- `idempotencyKey` — `@NotBlank`, `@Size(max = 64)`
- `fromWalletId`, `toWalletId` — `@NotBlank`, `@Size(max = 64)` string
- `amount` — `@NotNull`, `@Positive` (BIGINT minor units)

**Status codes**

| Status | Condition |
|--------|-----------|
| 201 Created | First successful PROCESSED transfer |
| 200 OK      | Idempotent replay (cached response returned) |
| 422         | Insufficient funds — transfer persisted as `FAILED`, idempotent retries replay the failure |
| 400         | Validation failure or self-transfer |
| 404         | Source or destination wallet not found |
| 409         | Same idempotency key reused with a different payload, or concurrent duplicate that lost the unique-key race |
| 500         | Unexpected error |

Error envelope: `{ "error": "<CODE>", "message": "<human readable>" }`.

---

## 4. Database Schema

Defined in `src/main/resources/db/migration/V1__init.sql`.

**`wallets`**

- `id VARCHAR(64) PRIMARY KEY`
- `balance BIGINT NOT NULL CHECK (balance >= 0)`
- `created_at`, `updated_at TIMESTAMPTZ`

**`transfers`**

- `id UUID PRIMARY KEY`
- `from_wallet_id`, `to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id)`
- `amount BIGINT NOT NULL CHECK (amount > 0)`
- `status VARCHAR(16) NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED'))`
- `failure_reason VARCHAR(64)`
- `CHECK (from_wallet_id <> to_wallet_id)`
- Indexes on `from_wallet_id` and `to_wallet_id`.

**`ledger_entries`**

- `id BIGSERIAL PRIMARY KEY`
- `wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id)`
- `transfer_id UUID NOT NULL REFERENCES transfers(id)`
- `type VARCHAR(8) NOT NULL CHECK (type IN ('DEBIT','CREDIT'))`
- `amount BIGINT NOT NULL CHECK (amount > 0)`
- **`UNIQUE (transfer_id, type)`** — a transfer can produce at most one DEBIT
  and at most one CREDIT row. The service writes both entries in the same DB
  transaction as the transfer's PROCESSED state transition, so every PROCESSED
  transfer has exactly one of each.
- Index on `(wallet_id, created_at DESC)` for transfer-history reads.

**`idempotency_records`**

- `key VARCHAR(64) PRIMARY KEY`
- `request_hash VARCHAR(64) NOT NULL CHECK (char_length(request_hash) = 64)`
- `transfer_id UUID NOT NULL REFERENCES transfers(id)`
- `response_status SMALLINT` — nullable until the original response is
  committed
- `response_body TEXT` — cached JSON body returned on replay; stored as `TEXT`
  rather than `JSONB` so the byte-for-byte representation is preserved (JSONB
  re-serializes on read and can reorder keys)

### Why these constraints matter

Many invariants are enforced **structurally at the DB** so that even a bug in
the service layer cannot violate them:

| Invariant | Guard |
|-----------|-------|
| No negative balance | `CHECK (balance >= 0)` |
| No zero / negative transfer | `CHECK (amount > 0)` |
| At most one DEBIT + one CREDIT per transfer (service guarantees exactly two for PROCESSED) | `UNIQUE (transfer_id, type)` + service writes both entries in the same tx |
| No self-transfer | `CHECK (from_wallet_id <> to_wallet_id)` |
| No orphan ledger entries | `FK ledger_entries.transfer_id → transfers.id` |
| No duplicate idempotency keys | `PRIMARY KEY (key)` |

---

## 5. Idempotency Strategy

`request_hash` = SHA-256 over `(fromWalletId | toWalletId | amount)`.

The service flow for each request:

1. `findByKey(idempotencyKey)`:
   - **Record exists** → if `request_hash` matches and `response_body` is
     populated, return the cached body with HTTP 200. If `request_hash`
     differs, throw `IdempotencyConflictException(PAYLOAD_MISMATCH)` → 409.
   - **No record** → proceed.
2. Verify both wallets exist (404 if not). This is done before the transfer
   insert so a missing wallet surfaces as 404 rather than as a downstream FK
   error masquerading as 409.
3. Create the `Transfer` row in `PENDING`.
4. `saveAndFlush` an `IdempotencyRecord` with no response yet. If another
   thread beat us to inserting the same key, the unique-constraint violation
   surfaces as `DataIntegrityViolationException` and is mapped to 409
   `IDEMPOTENCY_IN_FLIGHT` after rollback.
5. Execute the transfer (see §6).
6. Serialize the `TransferResponse` DTO with Jackson and persist it on the
   idempotency record.
7. Return the body to the controller with the appropriate HTTP status.

The cached body is the **exact** JSON returned to the original caller —
replays return that body byte-for-byte (we store it as `TEXT`, not `JSONB`,
specifically to preserve key ordering).

**HTTP status on replay:** The controller always returns 200 on replay,
regardless of the original status (which could have been 201 or 422). The body
still carries the original `status` field (`PROCESSED` or `FAILED`), so the
caller can distinguish outcomes. This is a deliberate convention to make
replays distinguishable from fresh executions on the HTTP layer.

### Insufficient funds is still idempotent

If the source wallet does not have enough balance, the transfer is persisted
as `FAILED` with `failure_reason = INSUFFICIENT_FUNDS` and the idempotency
record stores the 422 response. Retries with the same key replay the same 422
deterministically — there is no temptation for the client to keep retrying a
doomed transfer.

---

## 6. Concurrency Strategy

**Isolation:** `READ_COMMITTED`. The critical section is deterministic and
deadlock-free under our explicit lock ordering, so `SERIALIZABLE` is not worth
the `40001` retry loop.

Both wallets are locked in ascending string id order. Two concurrent transfers
`A→B` and `B→A` therefore lock the same two rows in the same order — the
textbook reverse-direction deadlock cannot occur.

**Lock mode:** Hibernate's `PESSIMISTIC_WRITE` on Postgres maps to
`SELECT ... FOR UPDATE`. The lock is intentionally stronger than the default
read so concurrent debits serialize on the wallet row before the balance is
checked and updated.

### Why pessimistic over optimistic locking

The hot wallet in a transfer system is a high-contention row. With optimistic
locking, every concurrent debit on a hot wallet would either retry or fail; the
service would need an explicit retry loop with backoff. Pessimistic locking
serializes the writers behind a queue and keeps the retry logic out of the
application — simpler to reason about and easier to test.

---

## 7. Transfer State Machine

Defined on the `Transfer` entity:

```
PENDING ──markProcessed()──▶ PROCESSED  (terminal)
        ──markFailed(r) ──▶ FAILED      (terminal)
```

Invalid transitions throw `IllegalStateException` at the domain level. Because
the entire flow runs in one DB transaction, `PENDING` is transient on disk for
the happy path — but it is persisted so that future asynchronous workflows
(e.g. external clearing) can extend the model without schema changes.

Retries reuse the idempotency key and therefore never mutate a row that has
already reached a terminal state — the idempotency lookup short-circuits
before any wallet lock is acquired.

---

## 8. Failure Modes

| Failure | Effect |
|---------|--------|
| Validation failure | 400, no DB writes |
| Self-transfer | 400, no DB writes |
| Wallet not found | 404, no DB writes (checked before transfer insert) |
| Insufficient funds | 422, transfer persisted as `FAILED`, no ledger entries, idempotency record stored → replayable |
| Same key, different payload | 409 `IDEMPOTENCY_CONFLICT` |
| Concurrent duplicate that loses the unique-key race | 409 `IDEMPOTENCY_IN_FLIGHT` (caller should retry) |
| DB unreachable / crash mid-tx | Postgres rolls back; a client retry replays cleanly because the idempotency claim was rolled back too |
| Response lost in flight | Client retry hits the cached idempotency response and gets the original body |

---

## 9. Architecture / Layering

```
com.kullu.wallet
├── controller          // @RestController + @RestControllerAdvice
├── service             // @Transactional orchestration (TransferService)
├── repository          // Spring Data JPA, FOR UPDATE on wallets
├── entity              // JPA entities with domain-level state transitions
├── dto.request         // inbound DTOs with Bean Validation
└── dto.response        // outbound DTOs + ErrorResponse envelope
└── exception           // domain exceptions
```

Layering rules:

- **Controllers** validate, delegate, and map exceptions to HTTP status. No
  business logic.
- **Service** owns the `@Transactional` boundary and is the only place that
  orchestrates idempotency together with transfer execution.
- **Repositories** expose only persistence operations; no business decisions.
- **Entities** carry their own state-transition methods (`markProcessed()`,
  `markFailed(reason)`) so invalid transitions are impossible at the domain
  level.
- JPA entities never cross the controller boundary — DTOs sit at the edges.

---

## 10. Testing Strategy

Following the assignment's **Red → Green → Refactor** discipline.

### Unit tests (`./gradlew test`, no Docker)

- `TransferTest` — state machine: legal/illegal transitions, validation.
- `WalletTest` — debit/credit invariants, overdraft, validation.
- `LedgerEntryTest` — signed amount, constructor invariants.
- `RequestHasherTest` — determinism and field sensitivity (direction matters).

### Integration tests (`./gradlew integrationTest`, Testcontainers Postgres)

- `TransferControllerIT` — eight HTTP-level behaviours:
  - happy path → 201, balances updated, two ledger entries, ledger zero-sum
  - replay (same key + same payload) → 200, byte-identical body, no extra rows
  - replay with different payload → 409 `IDEMPOTENCY_CONFLICT`
  - insufficient funds → 422 + `FAILED` row + idempotency cache
  - insufficient funds replay → 200, identical body
  - self-transfer → 400 `SELF_TRANSFER`
  - missing wallet → 404 `WALLET_NOT_FOUND`
  - validation errors (negative amount, blank key) → 400

### Concurrency tests (`ConcurrencyIT`)

| ID | Invariant under test |
|----|----------------------|
| **C1** | **No double-spend.** 50 threads attempt to debit a wallet funded for exactly 30 transfers. Exactly 30 reach `PROCESSED`, 20 reach `FAILED(INSUFFICIENT_FUNDS)`, final balance is zero, ledger has 60 rows and sums to zero. |
| **C2** | **Idempotency under parallel duplicates.** 100 threads submit the same key simultaneously. Exactly 1 transfer row, 2 ledger rows, and all 100 response bodies are byte-identical. |
| **C3** | **Deadlock-free bidirectional load.** 100 transfers symmetric across A↔B run in parallel. All succeed (no `40P01`), net balance change is zero, ledger zero-sum. |
| **C4** | **Mixed workload invariants.** 80 threads, 40 sharing one key + 40 unique keys → 41 transfer rows, 82 ledger rows, balance equation `balance = initial + Σcredits − Σdebits` holds. |

Each concurrency test asserts both **externally observable** counts (HTTP
status counts, response bodies) and **persisted invariants** (row counts,
ledger zero-sum, balance equation).

**Total: 31 tests, all passing on Postgres 16.**

---

## 11. Tradeoffs & Non-Goals

- **Stored balance** chosen over deriving from the ledger — fast reads, simple
  insufficient-funds check; the ledger remains the audit trail. A
  reconciliation job is out of scope but trivial to add.
- **Pessimistic locking** chosen over optimistic locking (rationale in §6).
- **Synchronous single-transaction workflow** chosen over an async PENDING +
  worker design — simpler to reason about; the schema leaves room to evolve.
- **Cached response body as `TEXT`** rather than `JSONB` to preserve
  byte-for-byte equality across replays.
- **No outbox, no event publishing, no FX, no fees, no holds/reservations, no
  rate limiting, no auth.** All out of scope.
- **Observability:** structured Spring Boot logging exists; metrics and
  request-ID correlation are deferred (assignment's optional enhancements).

---

## 12. AI Disclosure

See [`AI_TRANSCRIPT.md`](./AI_TRANSCRIPT.md) for the full disclosure of how AI
tooling was used during this build.
