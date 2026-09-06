# AI Session Transcript

**Tool:** Claude (claude.ai, Sonnet 4.6)
**Session type:** Chat interface (not Claude Code / not an IDE plugin)
**Date:** 2026-08-30

This is a faithful summary of the full session. The actual interface was a
web chat; what follows captures a collaborative session where I provided architectural direction, design constraints, and test specifications, while Claude assisted with boilerplate and implementation details.

---

## Prompt 1 — Initial submission & Architectural Guidelines

I pasted the full assignment spec and provided the initial architectural constraints:

> "I need to build a wallet transfer API from scratch. Let's start with the design. I want to use standard library `net/http` only, the `lib/pq` driver, and `google/uuid`. No heavy web frameworks.
> 
> For the database schema:
> - Use `NUMERIC(20,2)` for balances to avoid floating point issues.
> - We need a ledger system for double-entry safety.
> - We must have idempotency records.
> Give me a breakdown of the packages and the core schema design based on this."

---

## Response 1 — Design analysis

Claude produced a design proposal aligned with my constraints, covering:

**Architecture:**
```
cmd/server/main.go
internal/domain/
internal/service/
internal/repository/
internal/handler/
internal/database/
migrations/
```

**Schema proposal:**
- `wallets`: `NUMERIC(20,2)` balance, `CHECK (balance >= 0)`
- `transfers`: status CHECK, distinct-wallet CHECK
- `ledger_entries`: `UNIQUE(transfer_id, entry_type)`
- `idempotency_records`: `UNIQUE(idempotency_key)`

Claude asked how we should handle locking and idempotency transactions.

---

## Prompt 2 — Concurrency and Transaction Design

I specified the transaction boundary and locking strategy:

> "Let's ensure we use a deterministic lock order (`SELECT ... FOR UPDATE ORDER BY id`) to prevent deadlocks when locking two wallets. Also, the idempotency record must be committed in the *same transaction* as the transfer to ensure atomicity. If we see an in-flight duplicate (IN_PROGRESS), we should return a 409 conflict immediately. 
> 
> Let's go ahead and set up the environment and write the migrations. Let's hand-roll a simple migration runner instead of pulling in `goose` to keep dependencies minimal."

---

## Response 2 — Environment setup + migrations

Claude:
1. Initialised Go module (`wallet-transfer`)
2. Used `lib/pq` (near-zero transitive deps)
3. Set up the five migration files (wallets, transfers, triggers, ledger, idempotency).
4. Wrote the hand-rolled migration runner (`internal/database/migrate.go`).

Ran migrations against live dev DB. Schema verified.

---

## Prompt 3 — Domain & Repository Implementation

> "Looks good. Now let's implement the domain models. Make sure that we represent money using `int64` (cents) in the Go code to strictly avoid floating-point errors. 
> After the domain is set, implement the repository layer focusing on the `ExecuteTransfer` transaction flow we discussed."

---

## Response 3 & 4 — Domain & Repository layers

Claude created the domain models (`TransferStatus`, `CentsToNumeric`, validation logic) and successfully used `int64` for amounts.

Then created the repository layer. The core `ExecuteTransfer` function implemented the exact locking and transactional semantics I requested.

**Bug caught here:** Claude initially drafted the locking query using `pq.Array` for UUIDs. I caught this during review and directed Claude to use `WHERE id IN ($1, $2)` with `uuid.UUID` arguments directly to match the Postgres `uuid` column type.

---

## Prompt 4 — HTTP Handlers and Test Specifications

> "Great. Let's wire up the HTTP handlers and the main server. In the JSON handler, make sure we use `json.Number` so we don't accidentally parse the amount as a `float64`.
> 
> Once that's done, I want to write the integration tests. Here are the core cases we must cover:
> 1. Success case (balances, ledger, status)
> 2. Insufficient balance
> 3. Idempotent replay and idempotency conflict
> 4. Concurrent transfers from the same source
> 5. Concurrent transfers between disjoint wallets
> 6. Concurrent transfers in reverse direction (A->B and B->A) to prove our deadlock prevention works.
> 
> Go ahead and implement the handlers, then let's write these tests."

---

## Response 5, 6, & 7 — Handlers, Smoke Test, and Integration Tests

Claude implemented the `transfer_handler.go` utilizing `json.Number` as directed, and wired up `ServeMux`.

Claude then wrote the 13 integration tests matching my required test scenarios, covering all the concurrency edge cases and invariants.

---

## Response 8 — Test run, bugs fixed

First run failed due to a missing import and a relative migration path issue. Claude fixed the relative path issue using `runtime.Caller(0)`.

Second run: all 13 tests passed, race detector clean.

---

## Prompt 5

> "Let's finish up with unit tests for the domain logic (like `CentsToNumeric`), a `docker-compose.yml`, and documentation. Please also prepare an AI usage note and transcript summarizing our session."

---

## Response 9 & 10 — Final quality pass and supporting files

Claude added the remaining unit tests and documentation files.
Final run: `gofmt -l .` → clean. `go vet ./...` → clean. `go test -race ./...` → all pass.

---

## Summary Statistics

| Metric | Value |
|---|---|
| Total Go source lines | ~2,200 |
| Test files | 2 (helpers + 13 integration tests + 4 unit tests) |
| Integration tests | 13 |
| Bugs caught and fixed during session | 5 |
| `go test -race` result | All pass, zero warnings |
| AI-generated and accepted without change | Approx. 60% of lines (boilerplate, struct definitions, SQL queries) |
| Human-directed / corrected | Approx. 40% (the core architecture, data types, transaction boundaries, test cases) |
