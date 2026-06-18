# AI Session Transcript — Wallet Transfer Service

**Tool:** Claude Code (Anthropic)

---

**1.** Read `ASSIGNMENT.md`, `evaluation_guide.md`, and the PR template. Produce a full
implementation plan — schema, idempotency strategy, concurrency strategy, API contract, layering,
and testing strategy — before any code is written.

→ Cross-referenced all three documents and produced a structured plan covering every evaluation
criterion. Key decisions surfaced at this stage:

- Idempotency enforced via `UNIQUE` constraint on `transfers.idempotency_key` plus a
  `request_fingerprint` column (SHA-256 of the request payload), rather than a separate
  `idempotency_records` table. Fewer moving parts; no risk of the idempotency record and the
  transfer row disagreeing. Flagged explicitly as a tradeoff from the suggested four-table schema.
- Concurrency control via pessimistic row locking (`SELECT ... FOR UPDATE`) over optimistic
  locking. Reasoning: wallet transfers are write-heavy and contention-prone; row locks give
  predictable blocking rather than retry storms under high concurrency.
- Insufficient funds modeled as a committed `FAILED` transfer (HTTP 201), not a 4xx error —
  it is a valid business outcome, not a malformed request.

Plan reviewed and approved before implementation started.

---

**2.** Define binding engineering rules for this project — stack, layering, database design
standards, and development phases — so every subsequent change follows the same discipline.

→ Authored `CLAUDE.md` covering: Java 17 / Spring Boot 3.2.5 stack, explicit SQL via
`NamedParameterJdbcTemplate` (no ORM), constraint-first database design, fixed-precision money
(`NUMERIC(19,4)` / `BigDecimal`), pessimistic locking with deterministic lock ordering, and
an ordered set of development phases each with a stated exit criterion.

---

**3.** Implement the schema as Flyway migrations. Every invariant that can be expressed as a
database constraint must be — do not rely on application code to enforce what the database can
guarantee.

→ Produced `V1__init_schema.sql`: `wallets`, `transfers`, `ledger_entries` with `CHECK`,
`UNIQUE`, `FOREIGN KEY`, and `NOT NULL` constraints covering balance non-negativity, positive
transfer amounts, distinct wallet IDs, valid status values, and the one-DEBIT-one-CREDIT
ledger invariant (`UNIQUE(transfer_id, entry_type)`). Indexes added on wallet foreign keys.

---

**4.** Implement the domain layer — entities, enums, state-transition guards. No Spring
annotations, no persistence, no HTTP concerns.

→ Produced `Transfer`, `Wallet`, `LedgerEntry`, `TransferStatus`, `EntryType`, and `Money`.
State transitions enforced in the domain object (`markProcessed`, `markFailed`) via a guard
that rejects invalid sequences. Domain unit tests pass with no Spring context loaded.

---

**5.** Implement the repository layer — explicit SQL, row mappers, locking primitives. No
business rules.

→ Produced `WalletRepository` (including `lockById` for `SELECT ... FOR UPDATE`),
`TransferRepository`, and `LedgerEntryRepository`. No balance checks or status logic in
this layer.

---

**6.** Implement the service layer — idempotency handling, lock ordering, transaction boundaries,
transfer workflow. Treat everything here as unproven until a concurrency test exercises it.

→ Produced `TransferService` (idempotency lookup and conflict detection) and `TransferExecutor`
(the locked transaction itself, in its own `@Transactional` method so a `DuplicateKeyException`
rolls back the entire unit of work, releasing wallet locks cleanly before the caller retries).

Lock ordering: wallets are always locked in ascending UUID order regardless of which is source
and which is destination, preventing deadlocks on opposite-direction concurrent transfers.

The concurrency integration tests (real Testcontainers Postgres, actual racing threads) surfaced
a genuine deadlock: the original design inserted the `transfers` row before acquiring the
`SELECT ... FOR UPDATE` locks. Postgres takes an implicit `FOR KEY SHARE` lock on referenced
wallet rows during an `INSERT` to enforce the foreign key; under concurrent load, multiple
transactions each holding a shared lock tried to upgrade to `FOR UPDATE` and blocked each other
indefinitely. Fixed by acquiring the row locks before the insert. Re-verified with the full
concurrency test suite.

---

**7.** Implement the handler layer — thin controllers, request/response DTOs with bean
validation, centralized exception-to-HTTP-status mapping.

→ Produced `TransferController`, `WalletController`, `TransferRequest`, `TransferResponse`,
`WalletResponse`, and `GlobalExceptionHandler`. Controllers call one service method and map
the result; no business logic or SQL in this layer.

---

**8.** Run the packaged application against a real Dockerized Postgres (not Testcontainers) and
exercise every documented API behavior by hand — happy path, idempotent replay, idempotency
conflict, insufficient funds, unknown wallet, validation errors.

→ Manual verification caught a `BigDecimal` scale inconsistency: a freshly created transfer
returned `"amount": 100` while the same record reloaded from the database returned
`"amount": 100.0000`. Fixed by normalizing all monetary amounts to a fixed scale in the domain
layer (`Money.normalize`). Re-verified both the automated suite and the manual run.

---

**9.** Final audit against the evaluation criteria — confirm every requirement is covered,
no code duplication, error handling complete, and the codebase matches what is documented
in `DESIGN.md`.

→ Found and extracted a duplicated UUID-parsing helper across two controllers into a shared
`RequestIds` utility. Confirmed `DESIGN.md`, the PR template, and the implementation are
consistent. Full test suite green.
