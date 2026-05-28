---
description: Walk through adding a new endpoint following the project's security and layering rules
---

You are adding a new HTTP endpoint to this wallet-transfer service. This
is a financial system — follow `AGENTS.md` strictly. If anything below
contradicts `AGENTS.md`, `AGENTS.md` wins.

## The user's request

$ARGUMENTS

If the request above is empty, ask the user:
- HTTP method and path
- Required input fields
- Expected response (success and failure shapes)
- Does this move money? (if yes, idempotency is required)
- Is there a state machine implication?

Do not start implementing until those are answered.

## Checklist

Implement in this order. Mark each step done as you go and explain
choices that are not obvious from the code.

### 1. Decide if a schema change is needed
- New table or column? Write a new numbered migration in `migrations/`
  AND `internal/db/migrations/`. Wrap in `BEGIN;`/`COMMIT;`. Include a
  down migration. Never edit a merged migration.
- New CHECK / UNIQUE / FK constraints that mirror application
  invariants — add them. Defence in depth.

### 2. Domain layer
- New entity, enum, or state — `internal/domain/`.
- New sentinel errors — `internal/domain/errors.go`.
- Domain has zero imports from handler/service/repository.

### 3. Repository layer
- One file per table. Methods take `(ctx, exec Executor, ...)`.
- Parameterized SQL only. No `fmt.Sprintf` into queries.
- Repositories never open transactions and never apply business rules.
- Unique-violation paths return the appropriate sentinel
  (`repository.ErrIdempotencyExists`, etc.).

### 4. Service layer
- Open the transaction via `TxManager.RunInTx`.
- If multiple wallets are touched, lock them in lexicographic id order
  with `SELECT ... FOR UPDATE` *before* reading state.
- If this is a money-moving endpoint, **idempotency is mandatory**:
  - Accept an `IdempotencyKey` in the request.
  - Fast-path replay via `idempotency_records` outside the tx.
  - Slow-path serialise via the PK unique-violation inside the tx.
  - Same key + different body → `domain.ErrIdempotencyConflict`.
- Re-validate inputs in the service even though the handler already did
  (defence in depth).

### 5. Handler layer
- Decode JSON with `dec.DisallowUnknownFields()`.
- Call the service. Encode the result.
- No SQL, no business rules, no transactions.
- Route in `internal/handler/router.go`.

### 6. Error mapping
- Extend `classify` in `internal/handler/errors.go` for any new sentinel
  error.
- Client responses never expose DB errors, schema names, or DSN.
- 500s send `"internal error"` to the client; full detail goes to
  `slog.ErrorContext`.

### 7. Tests
- Domain unit tests if logic was added in domain.
- Service integration test (build tag `//go:build integration`) — happy
  path and at least one failure mode.
- Handler integration test — HTTP shape, error mapping, status codes.
- If the endpoint touches concurrently-mutable state, add a goroutine
  load test that asserts the invariant (no over-spend, no duplicate
  side effect).
- Run `go test -race -tags=integration ./...` and confirm clean.

### 8. Documentation
- Update the API reference table in `README.md`.
- Update the test coverage matrix in `README.md`.
- If the design has notable tradeoffs, add them to the README's
  "Tradeoffs and assumptions" list.

## Before you mark the work done

Self-audit:

1. Could a duplicate retry produce a double effect? (idempotency)
2. Could two concurrent calls race? (locks + tx)
3. Is every query parameterized?
4. Are CHECK / UNIQUE constraints catching the invariant at the DB
   layer too?
5. Does any error string returned to the client leak schema details?
6. Did you add tests that fail without your change?
7. Did you update the README API table?

Then run `/secure-review` and address every BLOCKING / WARNING.

Do not commit on the user's behalf unless explicitly asked.
