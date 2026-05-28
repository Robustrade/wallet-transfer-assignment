# AGENTS.md

Instructions for any AI coding assistant (Claude Code, Cursor, Aider,
Continue, OpenCode, etc.) working on this repository. This is the
canonical, vendor-neutral file; `CLAUDE.md` is a one-line pointer that
defers to this one.

This is a **financial system** that moves money between wallets. Correctness,
security, and audit-friendliness come before cleverness, throughput, or
brevity. When two rules appear to conflict, the one closer to *security*
wins.

If you are unsure whether a change is safe, stop and ask the user. Asking
is cheap; a duplicate debit is not.

---

## 0. Project context (read first)

- **Language / runtime:** Go 1.24+ (the floor declared in `go.mod`;
  bumping pgx/v5 ≥ 5.9.0, OpenTelemetry ≥ v1.42.0, or
  testcontainers-go ≥ v0.41.0 would force it up to 1.25).
- **HTTP:** stdlib `net/http` with Go 1.22 method-aware `ServeMux`. No web
  framework.
- **Database:** PostgreSQL 16 via `database/sql` + the `pgx` driver. No ORM.
- **Schema migrations:** `golang-migrate`, SQL files embedded via
  `go:embed`. Forward-only.
- **Tests:** integration tests are gated by `//go:build integration` and
  read `DATABASE_URL` (a real Postgres provided by docker-compose locally
  or the CI `services.postgres` block). No testcontainers — that
  dependency would force the Go floor up to 1.25.
- **Architecture:** strict layering, dependencies point inward
  (`handler → service → repository → DB`), with `domain` as a leaf package.

For the full design rationale read [README.md](./README.md). For the
assignment text read [ASSIGNMENT.md](./ASSIGNMENT.md). For the reviewer
rubric read [evaluation_guide.md](./evaluation_guide.md).

---

## 1. SECURITY — highest priority

These rules are **non-negotiable** unless the user explicitly overrides
them in a single, in-conversation instruction. Re-read this section before
every meaningful change.

### S-1. SQL must be parameterized — always

```go
// ✅ correct
exec.ExecContext(ctx, `UPDATE wallets SET balance = $1 WHERE id = $2`, balance, id)

// ❌ NEVER
exec.ExecContext(ctx, fmt.Sprintf(`UPDATE wallets SET balance = %d WHERE id = '%s'`, balance, id))
```

There is no exception. Even "trusted internal" input, even "just a debug
query", even `ORDER BY` clauses (use an allowlist switch instead). A
single concatenated query in a financial system can move money to an
attacker.

### S-2. Money math uses integers, never floats

- ✅ `int64` minor units (cents).
- ❌ `float64` for any monetary value. Ever.

Floats silently lose precision on values you would not expect; the
rounding error always lands on the user's side of the ledger.

### S-3. Mutations live inside a transaction

Any operation that touches more than one row, or touches one row in a way
that depends on a prior read, runs inside `TxManager.RunInTx`:

```go
err := txm.RunInTx(ctx, func(exec repository.Executor) error {
    // 1. SELECT ... FOR UPDATE on every row whose state will depend on a read
    // 2. read state
    // 3. validate invariants
    // 4. mutate
    return nil
})
```

If you find yourself reading a balance, then computing the new balance,
then writing it back *without* a `FOR UPDATE` lock on that row, you have
a TOCTOU race that can drain a wallet under concurrent load.

### S-4. Lock wallets in deterministic order

Always lock in lexicographic order by id. The helper `orderedPair` in
`internal/service/transfer.go` exists for this — use it. Locking
"source then destination" creates a deadlock when one transfer goes
A→B while another goes B→A.

### S-5. Validate at every boundary (defence in depth)

- **Handler layer** decodes JSON with `dec.DisallowUnknownFields()` so a
  typo'd field is rejected, not silently dropped.
- **Service layer** re-validates the same invariants (services may be
  called from other contexts later — workers, gRPC, scripts).
- **Database layer** enforces invariants with `CHECK` / `UNIQUE` / `FK`
  constraints regardless of what the application does.

Validation must cover at minimum:
- Required fields present.
- `amount > 0`.
- `idempotencyKey != ""`.
- `fromWalletId != toWalletId`.

### S-6. Never log secrets or full request bodies

- `DATABASE_URL` contains credentials. Do not log it.
- Do not log full POST bodies — log a redacted summary (`fromWalletId`,
  `toWalletId`, `amount`) if needed.
- Stack traces with full SQL queries can be a credential leak when the
  DSN appears in the connection error. Catch and replace before logging.

### S-7. Errors do not leak internals to clients

```go
// ✅ correct
writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
slog.ErrorContext(r.Context(), "transfer failed", "err", err)   // full detail to logs

// ❌ NEVER
http.Error(w, err.Error(), 500)   // leaks pq error text, schema names, sometimes DSN
```

All client-facing error translation lives in `internal/handler/errors.go`.
Add a `classify` case there; do not bypass it.

### S-8. External identifiers are parameters, never interpolated strings

Path params from `r.PathValue("id")`, JSON body fields, query strings —
all of them flow through SQL placeholders. Never `fmt.Sprintf` them into
a query, never embed them in a `LIKE` pattern without escaping, never
hand them to `exec.Command` arguments.

### S-9. Idempotency is mandatory on any money-moving endpoint

Every new endpoint that creates a domain object whose existence has a
financial effect MUST:
- Require an `idempotencyKey` from the caller.
- Use the same "insert into a UNIQUE-keyed table inside the tx" pattern
  as `internal/service/transfer.go`.
- Return the same response for replays.
- Return `409 Conflict` for same-key-different-body.

Money movement without idempotency is a duplicate-charge incident waiting
to happen.

### S-10. Authentication / authorization (out of scope today, but…)

`POST /wallets` is intentionally unauthenticated for the assignment. **Do
not** extend that pattern. Any new mutating endpoint must:
- Either reject anonymous requests, or
- Be explicitly documented as "test convenience only" in both README and
  ASSIGNMENT.md.

Never add an endpoint that allows arbitrary balance edits.

### S-11. Dependencies are vetted before adding

Before adding any new module:

1. Is this in the standard library? Use that instead.
2. Is it widely used and actively maintained? Check release cadence,
   recent issues, stars, security advisories.
3. Read its `go.sum` impact — how much transitive surface does it pull
   in?
4. Run `govulncheck ./...` if available.

We deliberately have **no web framework, no ORM, no auth library**. Keep
it that way unless there is a strong reason and the user approves.

### S-12. Migrations are forward-only and reversible

- Up migrations only add or alter; destructive changes get a paired down
  migration.
- Down migrations exist and are tested.
- Migrations are wrapped in `BEGIN; ... COMMIT;`.
- Migration files are **never edited once merged**. Add a new file.
- Both copies (`/migrations` and `/internal/db/migrations`) must stay in
  sync; the second is what the binary embeds.

### S-13. Concurrency primitives are reviewed carefully

- Never hold a database transaction open across a network call, a long
  computation, or a `time.Sleep`.
- `context.Context` flows through every call. Never `context.Background()`
  inside request-scoped code.
- Goroutines that touch shared state use channels, `sync.Mutex`, or
  `sync/atomic` — never bare maps.

### S-14. Inputs to `exec.Command` / `os/exec` are validated against an allowlist

We do not currently shell out from production code. If you find yourself
adding `os/exec.Command(userInput, ...)`, stop. Almost certainly there is
a library function that does what you want without a shell boundary.

### S-15. `httpClient.Timeout` is set, every time

Any outbound HTTP client (we have none today) must have a `Timeout`. A
zero timeout means an attacker who controls a slow upstream can pin a
goroutine forever.

### S-16. pprof and other admin surfaces never share the public listener

`net/http/pprof` exposes goroutine dumps, heap state, CPU profiles, and
`cmdline`. It is served from a **dedicated listener** in
`cmd/server/main.go`, opt-in via `DEBUG_ADDR`, and `run()` logs a
`WARN` if the configured address is not loopback. Do not:

- Register pprof handlers on the public API mux (`internal/handler/router.go`).
- Set `DEBUG_ADDR` to `:6060` / `0.0.0.0:6060` outside local dev.
- Add new admin/diagnostic endpoints to the public mux. Use the debug
  mux in [`internal/handler/debug.go`](internal/handler/debug.go).

---

## 2. Architecture invariants

Strict layering. Dependencies point inward only.

```
handler → service → repository → DB
              ↓
           domain  (no deps on the other layers)
```

### A-1. Handler does no business logic

Handler responsibilities are limited to:
- Decode the JSON / extract path params.
- Call exactly one service method.
- Encode the response (or hand off to `writeError`).

What handlers **must not** do: open transactions, call SQL directly, do
balance arithmetic, embed business rules.

### A-2. Service is the only layer that opens transactions

`internal/repository/postgres/TxManager.RunInTx` is the single
transaction primitive. Only the service layer calls it.

Repositories accept an `Executor` (satisfied by both `*sql.DB` and
`*sql.Tx`) and never call `BeginTx`.

### A-3. Repository is SQL only

- No business rules.
- No `if balance < amount` checks — that is service-layer.
- One file per table.
- First arg after `ctx` is the `Executor`, so each method works inside
  or outside a tx.

### A-4. Domain has zero external imports (stdlib only)

`internal/domain/` cannot import from `internal/repository/`,
`internal/service/`, or `internal/handler/`. If you are about to add a
DB-shaped struct field to a domain entity, stop — that struct belongs
in the repository layer.

### A-5. Errors flow up as sentinels, matched with `errors.Is`

- Domain errors live in `internal/domain/errors.go`.
- Repository errors live in `internal/repository/repository.go`
  (`ErrNotFound`, `ErrIdempotencyExists`).
- The handler maps both with `errors.Is`. Never match on error message
  strings — they break the moment someone wraps an error.

---

## 3. Database conventions

- Always parameterize (see S-1).
- Always `SELECT ... FOR UPDATE` when the row will be mutated based on
  the read.
- Always declare invariants at the schema level (`UNIQUE`, `CHECK`,
  `FOREIGN KEY`) **in addition to** the application check. Defence in
  depth.
- `BIGINT` for monetary amounts (see S-2).
- `TIMESTAMPTZ` for times, never `TIMESTAMP` (without TZ).
- New schema changes go in a new numbered migration file. Never edit a
  merged one.
- After updating SQL files in `migrations/`, copy them to
  `internal/db/migrations/` (the embedded copy) and verify both are
  identical.

---

## 4. Concurrency rules

- New code that mutates a wallet MUST lock the row first.
- New code that touches multiple wallets MUST lock in id-ascending order
  (see S-4).
- Never block on I/O while holding a transaction.
- Every blocking call accepts a `context.Context` and honours
  cancellation.
- Hot-path tests assert that 20+ concurrent workers do not drain a
  wallet or duplicate a transfer (`TestCreateTransfer_ConcurrentDebits`,
  `TestCreateTransfer_ConcurrentSameKey`).

---

## 5. Idempotency rules

- Every mutating endpoint that produces a financial side effect accepts
  an `idempotencyKey`.
- The key is stored in a table whose PK is the key itself — that PK is
  what serialises concurrent first-time inserts.
- Replays return the same response (same id, same state, same body).
- Replays with a different request body return `409 Conflict`.
- Idempotency and the side effect commit in the same transaction so they
  cannot diverge.

---

## 6. Testing rules

- **Unit tests** live next to the code (`*_test.go`, no build tag).
- **Integration tests** are gated with `//go:build integration` and use
  `internal/testdb`, which connects to the Postgres pointed at by
  `DATABASE_URL` (skip the test if the env var is unset). Locally:
  `make db-up && make test-int`. In CI: the workflow runs a
  `postgres:16-alpine` service container and sets `DATABASE_URL`.
- New service / handler logic gets at least one integration test.
- Concurrency-sensitive code gets a multi-goroutine test that asserts
  the invariant under load.
- Use `testdb.Reset(t)` between subtests; do not leave residue.
- Race detector must be clean:
  `go test -race -tags=integration -count=1 ./...`.

---

## 7. Code style

- `gofmt -w .` before every commit; CI fails on `gofmt -l .` output.
- `golangci-lint run ./...` clean.
- Package comments on every new package.
- **Commenting style is tutorial-grade.** A reviewer who has never
  opened this codebase should be able to read top-to-bottom and
  understand what each layer is doing and why. That means:
  - Every exported identifier has a doc comment.
  - Every non-trivial function body has step-labeled or
    block-explaining comments — they describe *intent*, not just
    restating the code.
  - Each test starts with a comment naming the scenario under
    verification and the invariant the assertions enforce.
  - Migrations, SQL constraints, and security-sensitive paths
    (idempotency, locking, recovery) carry inline explanations of
    why the choice was made, not just what it does.
- Comments must still earn their keep: avoid restating obvious names
  (`// constructor`), avoid TODO/HACK markers that have no owner,
  avoid commenting out code (delete it; git has the history).
- Prefer:
  - Standard library over third-party.
  - `fmt.Errorf("op: %w", err)` over bare `return err`.
  - `errors.Is` / `errors.As` over string matching.
  - Named constants over magic strings sprinkled around.

---

## 8. Commit and PR conventions

- One logical change per commit. Refactors get their own commits.
- Subject line: imperative mood, ≤ 72 chars (`Fix idempotency replay`
  not `fixed idempotency` or `Fixes the bug where…`).
- Body explains *why*.
- Never `git commit --no-verify`. Hooks exist for a reason — fix the
  underlying issue.
- Never `git push --force` to `main` or any shared branch.
- PRs populate every section of
  [`.github/pull_request_template.md`](./.github/pull_request_template.md),
  including the AI disclosure.
- Branch name: `solution/<your-name>` (per ASSIGNMENT.md).

---

## 9. What NOT to do

| Anti-pattern | Why banned |
|---|---|
| `fmt.Sprintf("SELECT … WHERE id = '%s'", id)` | SQL injection (S-1) |
| `float64` for `amount` | Precision loss (S-2) |
| `SELECT balance` then `UPDATE balance` outside a tx | TOCTOU race (S-3) |
| `SELECT … FOR UPDATE` in handler code | Layering (A-1, A-2) |
| Skipping `idempotencyKey` on a money-moving endpoint | Duplicate charges (S-9) |
| Editing a merged migration file | Out-of-sync schemas (S-12) |
| `git commit --no-verify` | Hooks exist for a reason |
| `git push --force` to main | Rewrites history, drops other people's work |
| Adding an ORM | We deliberately don't use one — propose in a separate discussion first |
| Returning a raw DB error to the client | Information leak (S-7) |
| Logging full request body | Secret leak (S-6) |
| `context.Background()` inside request-scoped code | Loses cancellation / deadlines |
| Holding a tx open across `http.Client.Do` | Connection-pool exhaustion + lock-pinning |

---

## 10. When asked to add a new endpoint

Default checklist:

1. **Domain** — does this need a new entity / state / error?
2. **Migration** — does this need a schema change? New numbered file in
   `migrations/` AND `internal/db/migrations/`.
3. **Repository** — SQL-only methods on a new or existing repo.
4. **Service** — transaction boundaries + business rules + idempotency.
5. **Handler** — decode, call service, encode. Update `router.go`.
6. **Errors** — extend `classify` in `internal/handler/errors.go` if you
   added new sentinel types.
7. **Tests** — domain unit + service integration + handler integration.
8. **Docs** — update API reference and the test coverage matrix in
   [README.md](./README.md).

---

## 11. When to ask before acting

Always ask the user before:

- Dropping a database table or column.
- Editing CI configuration or branch protection.
- Adding a new module dependency.
- Disabling a `CHECK` / `FOREIGN KEY` / `UNIQUE` constraint.
- Changing the idempotency or locking strategy.
- Touching anything that could affect money-flow correctness.
- Force-pushing, hard-resetting, or rewriting any history.
- Bypassing pre-commit hooks.

---

## 12. Common commands

```sh
make db-up         # local Postgres on :5432
make db-down       # stop it
make run           # build + run server on :8080
make test          # unit tests
make test-int      # integration tests (Docker required)
make test-all      # both
make lint          # golangci-lint
make fmt-check     # gofmt verification
make tidy          # go mod tidy
```

---

## 13. Skills and slash commands available in this repo

Agent assistants reading this file should be aware of the following
project-scoped capabilities. They auto-load on matching context (skills)
or run on explicit invocation (commands). All live under `.claude/` for
Claude Code; the conventions are vendor-neutral and easily portable to
other agents.

### Skills (auto-loaded on matching context)

| Skill | Loads when… | What it provides |
|---|---|---|
| [`postgres-tx-safety`](.claude/skills/postgres-tx-safety/SKILL.md) | Editing SQL, transactions, locking, or balance logic (anything under `internal/repository/`, `internal/service/`, `migrations/`) | Parameterised-SQL templates, RunInTx + lock-order patterns, idempotency template, SQLSTATE map, anti-pattern table |
| [`wallet-migration`](.claude/skills/wallet-migration/SKILL.md) | Adding or altering tables, columns, indexes, CHECK / UNIQUE / FK constraints | 6-step authoring workflow including the dual `migrations/` + `internal/db/migrations/` sync, patterns for the common DDL operations, house rules |
| [`wallet-integration-test`](.claude/skills/wallet-integration-test/SKILL.md) | Writing or modifying integration tests in `internal/service/` or `internal/handler/` | `DATABASE_URL`-backed `testdb.Get` + `testdb.Reset` patterns, service-level / HTTP / concurrency / same-key collapsing test templates |

### Slash commands (explicit invocation)

| Command | Purpose |
|---|---|
| [`/secure-review`](.claude/commands/secure-review.md) | Walk the current `git diff` against the full security checklist in §1; report findings as BLOCKING / WARNING / NOTE |
| [`/new-endpoint`](.claude/commands/new-endpoint.md) | Walk through the layered build order (domain → migration → repo → service → handler → tests → docs) for a new HTTP endpoint, with idempotency mandatory for money-movers |

### Governance

- New skills follow the action-oriented naming pattern
  (`wallet-migration`, `postgres-tx-safety` — not `db-stuff`).
- Skills and commands are versioned with the codebase. Changes go
  through the same PR review as application code.
- Skills are kept tightly scoped: one trigger context, one concern.
  Cross-cutting concerns belong in [AGENTS.md](./AGENTS.md), not in a
  skill.
- When a skill becomes obsolete, mark it deprecated in its description
  for one release cycle before removing the directory, so anyone
  reading recent git history sees the deprecation rather than a
  silent disappearance.

---

## 14. Mental model for code review

When reviewing a diff (your own or another contributor's), walk through
this checklist:

1. **Money safety:** can this drain or duplicate a balance?
2. **SQL safety:** is every query parameterized?
3. **Transaction safety:** are mutations atomic with their reads?
4. **Lock safety:** are rows locked in deterministic order?
5. **Idempotency:** can a retry produce a duplicate side effect?
6. **Information disclosure:** can an error message leak schema/DSN/PII?
7. **Layering:** does anything skip a layer or do work in the wrong one?
8. **Tests:** does the new behaviour have a failing-without-the-change
   test backing it?

If any answer is "I'm not sure", stop and ask.
