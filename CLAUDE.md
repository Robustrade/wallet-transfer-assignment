# Wallet Transfer Service — Project Rules

Binding conventions for this codebase. Follow these for every change unless the user
explicitly overrides one in conversation.

## Stack

- Java 17, Spring Boot 3.2.5
- Persistence: `spring-boot-starter-jdbc` with `NamedParameterJdbcTemplate` and explicit SQL.
  **No JPA/Hibernate, no ORM.** Locking and transaction semantics must stay visible in the
  SQL, not hidden behind an ORM.
- Database: **PostgreSQL** (driver only at runtime). Tests run against a real Postgres via
  **Testcontainers** — never mock the database in service/integration tests.
- Migrations: **Flyway**, versioned `V{n}__description.sql` under `src/main/resources/db/migration`.
  Schema changes always go through a new migration file, never an edit to an applied one.
- Lombok for boilerplate (`@Value`, `@Builder`, `@RequiredArgsConstructor`) — do not hand-write
  getters/setters/constructors it can generate.

## Package layout (`com.wallet.*`)

Matches the assignment's own terminology (handler/service/repository/domain layers):

```text
handler      REST controllers, request/response DTOs, @ControllerAdvice exception mapping
service      orchestration, business rules, idempotency, transaction boundaries
repository   persistence only — JdbcTemplate calls, row mappers, SQL
domain       entities, enums, state-transition rules
exception    domain/business exceptions (WalletNotFound, InsufficientFunds, IdempotencyConflict, ...)
```

Rules:

- Controllers are thin: validate input, call one service method, map the result/exception to
  an HTTP response. No business logic, no SQL, no direct repository calls from controllers.
- Repositories never contain business rules (no balance checks, no status logic) — persistence
  only. They take/return domain objects, not DTOs.
- All business logic and transaction boundaries (`@Transactional`) live in the service layer.
- Domain objects own their own validity rules (e.g. valid state transitions) — don't scatter
  that logic into services or repositories.

## Database design rules

- Every table has a UUID primary key.
- Money is `NUMERIC(19,4)`, never `FLOAT`/`DOUBLE`. Java side uses `BigDecimal`.
- Every invariant that *can* be expressed as a DB constraint (`CHECK`, `UNIQUE`, `FOREIGN KEY`,
  `NOT NULL`) must be — don't rely on application code to enforce something the database can
  guarantee. This is the #1 thing the assignment rubric checks for.
- Ledger rows must never be able to exist without their parent transfer (`FOREIGN KEY`, no
  orphans) and a transfer must never produce more or fewer than one `DEBIT` + one `CREDIT`
  row (`UNIQUE(transfer_id, entry_type)`).
- Idempotency is enforced with a `UNIQUE` constraint on `transfers.idempotency_key` — durable,
  survives process restarts, no in-memory cache for this. We do **not** keep a separate
  `idempotency_records` table; the `transfers` row plus a `request_fingerprint` column is the
  idempotency record (deliberate simplification — call it out as a tradeoff if asked).
- Concurrency control is **pessimistic row locking** (`SELECT ... FOR UPDATE`), wallets always
  locked in deterministic id-sorted order to avoid deadlocks on opposite-direction transfers.
  No optimistic-locking version column — the row lock already serializes writers.
- Insufficient funds is a committed `FAILED` transfer (a business outcome), never a rolled-back
  transaction and never modeled as an HTTP 4xx.

## Testing

- Unit tests for domain logic (state transitions) need no Spring context.
- Service and controller tests run against Testcontainers Postgres — real constraints, real
  locking behavior. Mocking the datasource defeats the point of this assignment.
- Every concurrency claim (no double spend, no negative balance, no duplicate ledger rows on
  racing idempotent requests) must have a test that actually races threads, not just a
  single-threaded assertion.

## Development phases

Build and change this API in this order. Don't skip a phase, and don't reach backward into a
later layer to patch something that belongs in an earlier one (e.g. don't add a missing
constraint inside service-layer code — go back and add a migration). Each phase has an exit
criterion; don't move on until it's met.

**Phase 0 — Requirements & contract review.** Re-read `ASSIGNMENT.md`,
`evaluation_guide.md`, and `.github/pull_request_template.md` before touching code. Identify
every functional requirement, every evaluation question ("can duplicate transfers happen
accidentally?", "can ledger rows exist without a transfer?", etc.), and any point where the
assignment leaves a design choice open.
*Exit: a written plan — problem statement, API contract, idempotency strategy, concurrency
strategy, failure modes, testing strategy — reviewed before any code is written.*

**Phase 1 — Schema design.** Express the data model as Flyway migrations first. Apply the
Database design rules above: every invariant that can be a constraint, is one. No Java code yet.
*Exit: migrations apply cleanly against Postgres; the schema alone, read on its own, answers the
evaluation guide's schema questions.*

**Phase 2 — Domain layer.** Entities, enums, state-transition guards, validation. No Spring
annotations, no persistence, no HTTP concerns.
*Exit: domain unit tests pass with no Spring context loaded.*

**Phase 3 — Repository layer.** Persistence only — explicit SQL via `NamedParameterJdbcTemplate`,
row mappers, locking primitives (`lockById`, etc.). No business rules.
*Exit: compiles against the domain layer; no balance/state logic has leaked in here.*

**Phase 4 — Service layer.** Orchestration, idempotency handling, transaction boundaries, lock
ordering. This is where correctness bugs are most likely (see the deadlock note above) —
treat anything here as unproven until a concurrency test exercises it.
*Exit: service/integration tests against real Testcontainers Postgres cover happy path,
idempotent replay, idempotency conflict, insufficient funds, unknown wallet, concurrent overdraft,
and concurrent opposite-direction transfers — and all of them actually pass.*

**Phase 5 — Handler/API layer.** Thin controllers, request/response DTOs with bean validation,
centralized exception-to-HTTP-status mapping.
*Exit: controller tests pass end-to-end (MockMvc + Testcontainers) for the documented status
codes (201/200/404/409/400).*

**Phase 6 — Verification.** Run `mvn verify`. Then run the actual packaged/booted app against a
real Postgres (not Testcontainers) and exercise every documented status code by hand (`curl`).
Any discrepancy found here is a real bug, not a test artifact — fix it and re-verify both the
automated suite and the manual run before calling the phase done.
*Exit: automated suite green, manual run matches the documented contract exactly.*

**Phase 7 — Documentation & disclosure.** Update `docs/DESIGN.md` to match what was actually
built (not what was originally planned, if they diverged). Fill out every section of
`.github/pull_request_template.md`. AI-disclosure must describe the methodology actually used,
not an idealized one.

**Phase 8 — Commit & PR.** Topical commits (schema, domain, repository, service, api, tests,
docs), only when the user explicitly asks for a commit — never commit proactively. PR description
covers schema design, idempotency strategy, concurrency handling, and tradeoffs.
