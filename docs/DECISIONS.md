# Wallet Transfer Service — Architecture Decision Record

**Status:** Living document — decisions accepted for the proposed implementation unless marked *Open*  
**Owner:** Candidate implementation  
**Source of truth:** `ASSIGNMENT.md` takes precedence over all proposed decisions.  
**Related:** `docs/REQUIREMENTS.md`, `docs/ARCHITECTURE.md`, `docs/DATABASE.md`, `docs/API.md`, `docs/TESTING.md` (when created).

## 1. Purpose

This document records **what we chose, why, alternatives considered, trade-offs, and consequences**. The assignment requires atomic transfers, concurrency safety, exactly two ledger entries for each successful transfer, idempotent behavior, state transitions, and documentation-first delivery. It does **not** mandate Fiber, GORM, Docker, or golang-migrate. Those are candidate design choices, not client requirements.

## 2. Decision summary

| ID | Decision | Status |
|---|---|---|
| ADR-001 | Go with Fiber v3; layered modular monolith | Accepted |
| ADR-002 | PostgreSQL with GORM | Accepted |
| ADR-003 | SQL migrations via golang-migrate | Accepted |
| ADR-004 | Docker Compose for run, migrations, and seed data | Accepted (tests still run on the host) |
| ADR-005 | Synchronous transfer in one PostgreSQL transaction | Accepted |
| ADR-006 | `READ COMMITTED`, deterministic wallet row locks | Accepted |
| ADR-007 | Durable idempotency key, payload fingerprint, response replay | Accepted |
| ADR-008 | Double-entry ledger and stored wallet balances | Accepted |
| ADR-009 | `PENDING` to terminal state within one transaction | Accepted |
| ADR-010 | Integer monetary amounts; no floating-point math | Accepted (unit open) |
| ADR-011 | Separate unit and PostgreSQL integration tests | Accepted |
| ADR-012 | Structured logs, health/readiness and bounded retries | Accepted |

---

## ADR-001 — Go, Fiber v3, layered modular monolith

**Decision:** Implement one Go HTTP service using Fiber v3. Organize code as HTTP handlers → application services → repository interfaces/implementations, grouped by feature under `internal/feature/wallet/{domain,model,repository,service,handler,route}`, with framework-independent domain types and rules. Inject dependencies through constructors.

**Why:** Clear separation of HTTP, orchestration, persistence, and invariants; simple deployment and straightforward testing.

**Alternatives:** Go `net/http`; other Go routers; microservices.

**Trade-offs:** Fiber adds a framework dependency; layers can become boilerplate if interfaces are invented unnecessarily. Keep abstractions tied to actual use cases.

## ADR-002 — PostgreSQL and GORM

**Decision:** PostgreSQL is the system of record. Use GORM for model mapping and ordinary queries; use explicit transactions, PostgreSQL row locks, and targeted SQL where necessary for correctness.

**Why:** PostgreSQL provides ACID transactions, unique constraints, row-level locking, and predictable concurrency semantics.

**Alternatives:** SQLite (permitted by assignment), `database/sql`, `pgx` without an ORM.

**Trade-offs:** GORM can obscure SQL and locking behavior. Review generated SQL and test transaction behavior against real PostgreSQL; do not rely on ORM conventions as a concurrency guarantee.

## ADR-003 — Versioned SQL migrations with golang-migrate

**Decision:** Maintain ordered `.up.sql` and `.down.sql` migrations under `migrations/`, applied with the golang-migrate CLI (`make migrate-up`). **Do not call `GORM.AutoMigrate` at application startup.**

**Why:** Schema is explicit, reviewable, reproducible, and independently versioned.

**Alternatives:** GORM AutoMigrate; manual SQL scripts; another migration tool.

**Trade-offs:** Migration files require maintenance and careful backward-compatibility planning. Destructive down migrations can lose data; use caution outside disposable test environments.

## ADR-004 — Docker-first, one-command reviewer experience

**Status:** Implemented for running the service: `make docker-up` starts PostgreSQL (creating the database), applies migrations, seeds 100 wallets and starts the API (`Dockerfile`, `docker-compose.yml`, `docker/seed/`). Tests are not containerised yet; `make test` and `make test-integration` run on the host (to target the Compose database, run `go test -tags integration ./internal/feature/wallet/integration/` directly with `DB_HOST=localhost DB_PORT=5433` and the `DB_USER`/`DB_PASSWORD`/`DB_NAME`/`DB_SSLMODE` values set; `make test-integration` always uses `.env`). The text below is the original intent.

**Decision:** Docker Compose runs PostgreSQL, a one-shot migration service, and the API. Provide a dedicated test service or profile for containerized tests. Pin compatible image and dependency versions; use multi-stage builds and a minimal non-root production image.

**Why:** A reviewer should be able to run the project with `docker compose up -d --build` and run tests with a documented Docker command, without installing Go or PostgreSQL on the host. The only host prerequisites are Docker and Compose.

**Alternatives:** Host-installed Go/PostgreSQL; shell scripts; a cloud-hosted environment.

**Trade-offs:** Requires Docker, sufficient resources, and available ports. Containerization reduces version drift but does not eliminate architecture, networking, or platform differences. Document environment defaults and provide a safe `.env.example`; do not commit real credentials.

**Operational acceptance:** Database health → migration success → API startup; failed migration must prevent API startup. Tests use an **isolated test database** and must not modify developer/demo data. `docker compose down` preserves the DB volume.

## ADR-005 — Synchronous, single-transaction transfer

**Decision:** Process `POST /transfers` synchronously. In **one PostgreSQL transaction**, claim the idempotency key, lock both wallets, create a `PENDING` transfer, validate funds, update balances, write exactly two ledger entries, transition to `PROCESSED`, and persist the response before commit. For a deterministic business failure, persist `FAILED` and its replayable response in the same transaction, without balance or ledger changes.

**Why:** A single commit boundary makes transfer effects atomic and allows safe replay after a lost HTTP response.

**Alternatives:** Persist `PENDING` first, then process asynchronously with workers; separate balance/ledger transactions.

**Trade-offs:** Wallet locks are held until commit; long transactions reduce throughput. No durable `PENDING` is visible after a process crash before commit. Async recovery workers are not needed for this design, though they would be useful for a future asynchronous workflow.

**Invariant:** An infrastructure failure before commit rolls back all effects; it must **not** be recorded as a durable business `FAILED` transfer. If commit outcome is uncertain, retry using the same idempotency key.

## ADR-006 — Deterministic row locking and transaction retries

**Decision:** Use PostgreSQL `READ COMMITTED`. Acquire `FOR UPDATE` locks on the two wallet rows **in ascending wallet-ID order**, regardless of transfer direction. Keep the transaction short. On SQLSTATE `40P01` (deadlock) or `40001` (serialization failure), retry the **whole transaction** with bounded attempts (3) and exponential backoff (100 ms, 200 ms).

**Lock before insert:** the wallet locks are taken *before* the `PENDING` transfer row is inserted. Inserting the transfer first takes `FOR KEY SHARE` locks on both wallets through the foreign keys; two concurrent transfers then each hold shared locks and deadlock when upgrading to `FOR UPDATE`. This was observed under a 10-request race test and fixed by reordering.

**Why:** Prevent double spending and reduce opposite-direction deadlocks while retaining a simple isolation model.

**Alternatives:** `SERIALIZABLE` for every transfer; optimistic version columns; conditional balance updates; advisory locks.

**Trade-offs:** High-traffic wallets can become contention points. Ordering reduces but does not categorically eliminate deadlocks from all database operations. Do not retry validation/business failures or blindly retry an ambiguous commit as a new operation.

## ADR-007 — Database-backed idempotency

**Decision:** The proposed API requires `idempotencyKey` for `POST /transfers`. Store a unique key, a canonical request fingerprint, and the original HTTP status/response for each committed outcome. Key reservation, transfer effects, and response storage participate in **the same transaction**.

**Behavior:**

- Same key + same canonical payload: replay the stored response; no new debit, credit, or ledger rows.
- Same key + different payload: `409 Conflict`; no new transfer.
- Same key, original not yet completed: `409 Conflict` ("still in progress"); the request is never re-executed.
- Replays are byte-identical: `response_body` is `JSONB`, which PostgreSQL re-formats, so the service re-serialises the stored body to canonical compact JSON.
- Concurrent requests using the same key: unique constraint coordinates contenders; a later request waits for the winner to commit or roll back, then rechecks the stored outcome.
- Crash before commit: reservation and effects roll back together; retry may safely execute.
- Crash after commit but before response: retry replays the committed outcome.

**Why:** Database uniqueness and atomic response persistence are more reliable than in-memory deduplication.

**Alternatives:** Redis TTL keys; in-memory locks; key-only uniqueness without payload comparison.

**Trade-offs:** Stored responses require retention and schema discipline. For this assignment, retain committed records indefinitely unless the contract is explicitly changed. The guarantee applies to requests carrying a key; it is not an exactly-once guarantee for arbitrary external side effects.

## ADR-008 — Stored balance plus double-entry ledger

**Decision:** Maintain wallet balances for fast reads and an auditable ledger. Each `PROCESSED` transfer writes **exactly two** ledger entries: one debit and one credit, equal amounts and linked to the same transfer. `FAILED` transfers write **no** ledger entries. Balance changes and ledger inserts commit together.

**Why:** Supports efficient balance checks while providing an accounting trail.

**Alternatives:** Derive balances exclusively from the ledger; single transfer-history row without ledger entries.

**Trade-offs:** Balances and ledger are redundant representations and must remain consistent. Database constraints cover row-local rules; exactly-two-entry and balancing rules require transactional application logic and verification tests (or additional DB enforcement).

## ADR-009 — Transfer state lifecycle

**Decision:** Valid transitions are `PENDING → PROCESSED` and `PENDING → FAILED`. Both terminal states are immutable. With ADR-005, `PENDING` exists during the transaction but normally is not externally observable as a committed state.

**Why:** Meets the required state machine without introducing a second commit or a recovery worker.

**Alternatives:** Commit `PENDING` separately and recover it asynchronously.

**Trade-offs:** A crash during processing leaves no persisted `PENDING` record; this is expected for the synchronous single-transaction design. Do not confuse transaction rollback with a committed `FAILED` business result.

## ADR-010 — Integer amounts

**Decision:** Represent money using integer units (`int64` in Go, `BIGINT` in PostgreSQL); require positive amounts, reject same-wallet transfers, and enforce nonnegative balances. Check arithmetic for overflow.

**Why:** Avoids floating-point rounding errors and simplifies equality of debit/credit entries.

**Alternatives:** Decimal types for currencies with variable precision; floating point (rejected).

**Trade-offs:** The assignment does not specify a currency or denomination. For tests, treat `amount` as an abstract smallest unit and document this assumption; real-money use requires a defined currency and scale.

## ADR-011 — Test strategy and red/green/refactor

**Decision:** Write failing tests before implementing nontrivial behavior, then implement and refactor. Separate fast unit tests from PostgreSQL-backed integration tests. Run them with `make test` (unit, in-memory fakes) and `make test-integration` (PostgreSQL).

**Required cases:** success, insufficient funds, invalid input, missing wallets, duplicate same-key replay, same-key/different-payload conflict, parallel duplicate requests, competing debits, opposite-direction transfers, ledger invariants, transaction rollback, and retry after a simulated lost response.

**Why:** Concurrency and atomicity guarantees cannot be established by handler mocks alone.

**Trade-offs:** Integration tests cost more time and require an isolated database. Prefer deterministic synchronization and assertions over arbitrary sleeps.

## ADR-012 — Observability and safe failure handling

**Decision:** Use structured `slog` logs, sanitized error responses, and a `/health` endpoint. Request IDs, per-transfer outcome logs and a `/ready` endpoint are planned but not yet implemented. Record transfer outcome, duration, retry counts, and DB errors without exposing credentials or sensitive payloads. Keep retries bounded and use context deadlines.

**Why:** Enables debugging and verifies failure handling without weakening safety.

**Alternatives:** Plain text logs; no readiness checks; unbounded retries (rejected).

**Trade-offs:** Instrumentation adds some complexity. Avoid logging raw secrets, database URLs, or unnecessary identifying data.

---

## 3. Open questions to resolve before affected implementation

These are **not** hidden requirements from the client:

1. **Wallet-not-found behavior (resolved):** `404`, transaction rolled back, no committed transfer, key not reserved.
2. **Wallet provisioning and initial funding:** Seed deterministic fixtures for evaluation, or add optional setup endpoints? Keep setup separate from transfer semantics.
3. **Identifier validation (partly resolved):** wallet IDs are positive integers; idempotency keys are 8–64 characters. The handler, domain and database all enforce 8–64. Open: request-size limits are undefined.
4. **Currency:** Confirm whether `amount` is abstract units or a specific currency's minor units.
5. **Idempotency key scope:** Global uniqueness is acceptable for the assignment; real multi-tenant systems should scope keys to authenticated principals.
6. **Timeouts and retry budget:** Select and test request, lock, statement, and transaction timeout values.
7. **Database invariants:** Decide whether extra triggers are warranted to enforce ledger immutability and cross-row balancing beyond application logic/tests.
8. **API error representation (resolved):** `{"error": "<message>"}` with `400` validation, `404` missing wallet, `409` insufficient funds / key conflict / in progress, `503` unconfirmed commit, `500` otherwise; `API.md` is authoritative.

## 4. Change control

Before implementing a nontrivial feature: review `ASSIGNMENT.md`, update the relevant contract/design note, add failing tests, implement, and verify. When changing an accepted decision, record its replacement and update `ARCHITECTURE.md`, `DATABASE.md`, `API.md`, and tests as appropriate. Do not silently change required client behavior.

## 5. AI-assisted development disclosure

The assignment requires the PR to disclose the AI tool used, how it was used, and the complete session transcript (or all prompts if a transcript cannot be exported). Preserve this material throughout development and include it with the PR as required by the client. This decision record is **not** a substitute for that disclosure.
