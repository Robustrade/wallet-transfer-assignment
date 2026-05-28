---
description: Security-focused review of pending changes on the current branch
---

You are doing a **security review** of the current git diff for this
wallet-transfer service. This is a financial system — your bar for
catching defects is much higher than for a typical web app.

## Inputs

Read these first:

1. `git status` — what changed
2. `git diff main...HEAD` (or `git diff` if not on a branch) — the actual
   changes
3. `AGENTS.md` — the project's security and architecture rules

If `git diff` is empty, say so and stop.

## What to look for

Walk every changed file against this checklist. Cite `file:line` for each
finding.

### Money safety
- [ ] No `float64` anywhere in monetary math. All amounts are `int64`.
- [ ] No code path can debit a wallet below zero (`balance < 0`).
- [ ] No code path can credit a wallet *twice* for the same transfer.
- [ ] Ledger always records exactly one DEBIT and one CREDIT per transfer,
      summing to zero.

### SQL safety
- [ ] Every query uses `$1`/`$2` placeholders. **Zero** uses of
      `fmt.Sprintf` / `fmt.Fprintf` / string concatenation into a query.
- [ ] `ORDER BY` / `LIMIT` / table names from caller input are
      allowlist-switched, not interpolated.
- [ ] No raw `db.Exec(userInput)`.

### Transaction safety
- [ ] Every read-then-write sequence runs inside `RunInTx`.
- [ ] Any wallet read whose value will be mutated uses
      `SELECT ... FOR UPDATE`.
- [ ] No transaction stays open across an HTTP call, file I/O, or
      arbitrary computation.
- [ ] No `context.Background()` inside request-scoped code.

### Lock safety
- [ ] Multiple wallets locked in lexicographic id order (via
      `orderedPair` or equivalent).
- [ ] Each new endpoint that touches multiple wallets has at least one
      concurrency test.

### Idempotency
- [ ] Any new mutating endpoint requires an `idempotencyKey`.
- [ ] Replay returns the same response.
- [ ] Same key + different body returns `ErrIdempotencyConflict`
      (`409 Conflict` from the handler).
- [ ] Idempotency row and side-effect row commit in the same transaction.

### Information disclosure
- [ ] Client error responses never include raw DB errors, SQL fragments,
      schema names, or the DSN.
- [ ] No `slog`/`log` call prints `DATABASE_URL`, the full request body,
      or any secret.
- [ ] All client-facing error translation flows through
      `internal/handler/errors.go` `writeError` / `classify`.

### Input validation
- [ ] Handlers use `json.NewDecoder(...).DisallowUnknownFields()`.
- [ ] Service layer re-validates the same invariants (defence in depth).
- [ ] CHECK / UNIQUE / FK constraints at the DB layer mirror the
      application's invariants.

### Authentication / authorisation
- [ ] No new unauthenticated mutating endpoint (`POST /wallets` is the
      single existing exception, documented as test-only).
- [ ] No new endpoint allows arbitrary balance edits, balance reads
      across users, or admin-style escalation.

### Migrations
- [ ] No merged migration is edited. Schema changes go in a new
      numbered file.
- [ ] Down migration exists for every up migration.
- [ ] `BEGIN; ... COMMIT;` wrapper on each migration.
- [ ] Both `/migrations` and `/internal/db/migrations` (embedded copy)
      are in sync.

### Dependencies
- [ ] Any new module addition is justified. Standard library was
      preferred where possible.
- [ ] `go mod tidy` was run and `go.sum` is up to date.
- [ ] No new dependency pulls in a known-vulnerable transitive — run
      `govulncheck ./...` if not already covered.

### Layering
- [ ] Handler does no business logic, no SQL, no transactions.
- [ ] Service is the only layer that calls `RunInTx`.
- [ ] Repository does no business rules.
- [ ] Domain has no imports from handler/service/repository.

### Tests
- [ ] Every new behaviour has at least one test that fails without the
      change.
- [ ] Integration tests use `testdb.Reset(t)` between subtests.
- [ ] Concurrency-sensitive code has a multi-goroutine test under
      `-race`.

## Output format

Group findings by severity. Inside each group, sort by file then line.

```
BLOCKING — must fix before merge
  - <file>:<line> — <one-line summary>
    <2-4 lines of context: why it is unsafe, how to fix>

WARNING — should fix but not blocking
  - <file>:<line> — <summary>
    <context>

NOTE — defensible but worth flagging
  - <file>:<line> — <summary>
```

If the diff has no findings in a category, omit that category.

If the diff is clean, end the report with the single line:

```
CLEAN — no findings against the security checklist.
```

Do not soften findings. Do not hedge. If something is unsafe, say so. If
you are uncertain, mark it as NOTE rather than skipping it.

## Argument handling

If the user passed an argument: $ARGUMENTS

If `$ARGUMENTS` is non-empty, scope the review to only files matching
that pattern. Otherwise review every changed file.
