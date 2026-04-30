# AI Usage — Session Prompts

The tool used was **Claude Code** (Anthropic's CLI for Claude). This file
lists the major prompts I sent during the session so the work is reproducible
from the prompts alone.

## Workflow

I drove the session in a strict **plan-then-execute** loop:

1. Asked Claude to *explore* the assignment repo and an internal Go service
   codebase from a prior project (used as a reference for engineering
   conventions) before writing any code.
2. Asked Claude to *plan* the implementation, refined the plan against
   my own logic, then explicitly approved the plan via `ExitPlanMode`.
3. Asked Claude to *implement* the solution incrementally, with `go test`
   and `go vet` after each layer.
4. Iterated through review passes (security, simplify) and applied focused
   fixes between each pass.

## Prompts (in order)

### Phase 1 — planning

1. *"go through the wallet-transfer-assignment code repository and plan the
   solution. Follow the [path to an internal Go service codebase, redacted]
   for practices to be followed. Always follow good practices like SOLID
   design principles, object-oriented design and design patterns."*

2. *"validate and refine the plan against my critical judgments listed
   below"* — communicated the architectural calls captured in
   "My critical judgments baked into the plan" (persistence choice,
   service layout, strict idempotency, single-Postgres ACID transaction,
   `READ COMMITTED + FOR NO KEY UPDATE`, idempotency-table collapse,
   `PENDING`-transient state machine, replay strategy, per-entity repos,
   `int64` minor units, no async worker). Claude's job was to draft and
   refine the plan against these constraints, not to choose them.

3. (Plan-mode answer to two clarifying questions:)
   - **Database:** PostgreSQL (recommended)
   - **Layout:** Mirror the internal reference codebase's
     `services/<name>/{init,route,internal/...}` layout

4. Approved the plan via `ExitPlanMode`.

#### My critical judgments baked into the plan

The architectural calls below were mine — driven by my own reasoning about
correctness, simplicity, and the assignment's scope. Claude's job in Phase 1
was to draft and refine the plan against these constraints, not to choose
them.

- **Persistence: PostgreSQL.** Picked for ACID transactions, FK + UNIQUE
  constraints, and CHECK as a last-line invariant — the cleanest fit for
  "correct under concurrency" without adding distributed-transaction
  machinery.
- **Service layout:**
  `services/<name>/{init,route,internal/{svc,repo,data}}`. Mirror the
  prior internal Go codebase rather than the simpler standard-Go
  `internal/{...}` layout, so service boundaries are explicit and the
  project is set up for a future multi-service refactor.
- **Strict idempotency: enforce `request_hash` in v1**, not as a deferred
  tradeoff. Same key + different body must return `409
  IDEMPOTENCY_CONFLICT` so a buggy client surfaces immediately instead of
  silently getting a stale outcome.
- **Concurrency: single Postgres ACID transaction with pessimistic row
  locking**, not Saga / TC/C / 2PC / event-sourcing / optimistic locking.
  The assignment is a single-node service — distributed-transaction
  protocols would be cost without benefit.
- **Isolation: `READ COMMITTED + SELECT FOR NO KEY UPDATE`** over
  `SERIALIZABLE`, with wallet IDs sorted lexicographically before the lock
  for deterministic acquisition order. Pessimistic locking is the
  canonical answer to the read-then-write race and avoids the `40001`
  retry loop. `FOR NO KEY UPDATE` (not `FOR UPDATE`) is mandatory because
  the in-flight `INSERT INTO transfers` already holds `FOR KEY SHARE` on
  the wallet rows from FK enforcement, and `FOR UPDATE` would deadlock
  with itself across concurrent transfers.
- **Idempotency table collapse:** the optional `idempotency_records`
  table from `ASSIGNMENT.md` is folded into `transfers.idempotency_key
  UNIQUE`. Same uniqueness guarantee, one less moving part.
- **State machine:** `PENDING → PROCESSED | FAILED`, with `PENDING`
  transient. The CHECK constraint allows it only so the initial INSERT is
  legal; under normal operation no committed row is ever `PENDING`.
  Business-outcome failures (insufficient funds, currency mismatch)
  **commit a FAILED row** rather than rolling back, so retries replay the
  failure instead of re-attempting a doomed transfer.
- **Replay strategy: block on the unique-index xmax lock and return the
  original committed outcome**, not a `429 Too Many Requests` mid-flight
  reply. Same exactly-once contract, fewer client retries, and the client
  gets the actual result on the first retry.
- **Per-entity repositories** (`Wallet`, `Transfer`, `Ledger`) rather
  than a single Store. Each repo has 2–3 methods, cohesion is fine, and
  this matches the prior internal codebase's per-entity pattern.
- **Money as `int64` minor units (paisa).** Never `float64`. JSON
  `amount` is decoded as a number per the assignment example; documented
  that a production-grade API would prefer string to avoid client-side
  float-precision risk past `2^53 - 1`.
- **No async worker / outbox / retry queue / DLQ** in v1. `PENDING` is
  transient; a process crash mid-transaction rolls back entirely,
  leaving no zombie rows to clean up.
- **Tests describe intended behavior, not the implementation.** Whenever
  a test was authored, I instructed Claude to write the assertions from
  the *contract* (what the API / function *should* do), not by reading
  the implementation it just produced. This catches the "tests pass
  because they mirror the bug" failure mode — if the implementation has a
  defect, the test still asserts the correct behavior and fails. Tests
  are a *check* on the implementation, not a transcription of it.

### Phase 2 — implementation

5. (After plan approval) Claude implemented the full solution from scratch:
   `go.mod`, migrations, `common/util/money`, `common/error`, domain models,
   request/response DTOs, repo interfaces + Postgres implementations,
   service + service tests, HTTP middleware, handler, route registration,
   config + infra, `cmd/server/main.go`, integration & concurrency tests,
   Dockerfile, docker-compose, Makefile, `.golangci.yml`, README.

### Phase 3 — review and refinement

6. *"are we missing anything?"* — caught the `currency=''` placeholder
   bug: the initial `INSERT` stored an empty currency that was never
   updated, so idempotent replays returned the wrong currency. Fixed in
   `UpdateOutcome`; the ledger-invariant integration test was strengthened
   to assert canonical currency on every transfer row.

7. *"review the code against the assignment's review prompts"* — self-
   review against `evaluation_guide.md` and `.github/copilot-instructions.md`.
   Wired the unused logger; tightened the ledger-invariant integration
   test.

8. *"fix these: currency CHAR(3) pads with spaces; Hash on DTO"* —
   switched the schema to `VARCHAR(3)` and moved `hashRequest` from the
   request DTO into the service package.

9. `/security-review` — multi-agent security pass. No HIGH/MEDIUM findings.

10. `/simplify` — three-agent code-quality / reuse / efficiency pass.
    Applied the high-leverage findings.

11. *"why can't we hardcode CI commands instead of repo variables?"* —
    hardcoded `make lint` / `make fmt-check` / `make test` directly into
    `.github/workflows/ci.yml` so a reviewer doesn't need to set
    GitHub Actions repo variables.

12. *"use gorm for postgres but keep the raw sql"* — swapped persistence
    from `database/sql` + `pgx` stdlib to `gorm.io/gorm`, while keeping
    every statement as hand-written parameterized SQL via
    `tx.Raw(...).Row().Scan(...)` / `tx.Exec(...)`. GORM is used purely
    as connection / transaction manager + raw-SQL executor; no ORM,
    AutoMigrate, hooks, or struct-tag scanning. `gorm.Config{Logger:
    Silent, SkipDefaultTransaction: true, TranslateError: false}` so pgx
    SQLSTATEs propagate verbatim.

13. *"run the full integration suite and let me know what breaks"* —
    running the integration + concurrency tests against a real Postgres
    surfaced a behavioral bug that the unit tests (with hand-written
    fakes) could not have caught. Fixed by changing the implementation,
    not by relaxing the test:

    - **`SQLSTATE 42883: operator does not exist: bigint + text`** in
      `ApplyBalanceDeltas` — the `UPDATE … CASE id WHEN $1 THEN $2 …`
      couldn't infer `$2`'s Postgres type from inside the `CASE` arm and
      resolved it as `text`. Fixed by adding `::bigint` casts on each
      `THEN` branch.

    Reinforces the testing methodology from Phase 1: tests written from
    the contract surface real defects; tests transcribed from the
    implementation would have masked them.
