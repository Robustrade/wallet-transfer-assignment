# AI Usage Disclosure

## 1. Tool Used

**Claude** (claude.ai, Sonnet 4.6) — Anthropic's web-based chat interface, not Claude Code or any IDE plugin.

This is relevant context: I used Claude as a **chat-based collaborator** in the browser, not as an autonomous agent with direct filesystem/shell access. Every command was run manually in a terminal on my side; every file was reviewed before it landed in the repo.

---

## 2. How I Generally Use AI for This Kind of Work

I use Claude as a senior pairing partner — specifically for:

- **Design review before coding.** I describe the problem and ask it to poke holes in my approach before I write a line. For this assignment that meant discussing idempotency strategy, locking order, and the transaction boundary before any schema was written.
- **Drafting boilerplate I'd write the same way every time.** Struct definitions, `database/sql` scan loops, JSON marshal/unmarshal wrappers — things where the interesting decision is the shape, not the syntax.
- **Surfacing edge cases I might miss.** Asking "what happens if the server crashes between commit and response?" or "what breaks if we don't lock wallets in deterministic order?" as a forcing function.
- **Not as a black box.** I don't paste a spec and accept the output. I read every function, question the choices that look wrong, and reject or rewrite the parts I disagree with.

What I don't use it for: trusting its output on subtle correctness questions (concurrency, financial precision) without understanding and verifying the reasoning myself. Those parts got the most scrutiny.

---

## 3. Session Transcript

The full transcript of the session that produced this code is in:

**`docs/ai_transcript.md`**

It includes every prompt and every response, in order. It is long (~the length of the entire session) because the assignment asked for it and because being selective would defeat the purpose.

A condensed summary of the key decision points from the session is below.

---

## Session Summary: Key Decision Points

### Phase 1 — Design Review (before any code)

I pasted the assignment spec and asked Claude to propose a schema, locking strategy, idempotency approach, and test plan, then **stop and wait for my approval** before writing anything.

The proposals it came back with:

| Topic | Claude's proposal | My decision |
|---|---|---|
| Money type | `int64` cents in Go + `NUMERIC(20,2)` in Postgres, no float anywhere, `json.Number` at JSON boundary | Accepted. This is the right answer and I verified the reasoning. |
| Idempotency | `INSERT ... UNIQUE` constraint as serialization point, idempotency record committed in same transaction as transfer | Accepted. The crash-after-commit safety argument is correct. |
| Locking | `SELECT ... FOR UPDATE ORDER BY id` on both wallet rows | Accepted. Deterministic order preventing deadlock is textbook; I verified it applies here. |
| Migration tool | Hand-rolled 50-line runner vs goose | I chose hand-rolled. Fewer dependencies for a self-contained assessment. |
| In-flight duplicate | Return 409 immediately vs poll | I chose 409. Simpler; correct. |
| Replay status code | Return original status code (201 or 409) vs always 200 | I chose original. Cleaner for the client. |

I approved the design before any code was written.

### Phase 2 — Incremental Implementation

Code was produced file by file in this order:

1. Migrations (SQL) → applied against live Postgres, schema verified via `\d`
2. `internal/domain/` — entities, state machine, money converter
3. `internal/repository/` — the core transaction logic
4. `internal/service/` — thin orchestration layer
5. `internal/handler/` — HTTP parsing, error mapping, router
6. `cmd/server/main.go` — wiring
7. Smoke test via curl (manual)
8. `tests/integration/` — all behavioral tests
9. `internal/domain/money_test.go` — unit tests

After each file: I read it, ran `go build && go vet`, and either accepted it or asked for a specific change. Notable corrections I made or directed:

- **Amount type at JSON boundary**: Initial draft used `float64` in the request struct. I flagged this as unacceptable for financial data. Changed to `json.Number` → `NumericToCents`, keeping float64 out of the entire codebase.
- **`pq.Array` for wallet lock query**: First draft used `pq.Array([]string{...})` to build the `WHERE id = ANY($1)` clause, which would have caused a type mismatch against a `uuid` column. I caught this and changed it to `WHERE id IN ($1, $2)`.
- **Relative migration path in tests**: Initial test helper used `"../../../migrations"`. This broke when `go test` ran from a different working directory. I directed the fix using `runtime.Caller(0)` to find the project root from the source file's location.
- **`go vet` warning in `TestTransfer_DecimalAmount`**: `resp` was used (`.Body.Close()`) before the error from `http.Post` was checked. Caught by `go vet`, fixed.
- **`gofmt` warnings**: Two files were reformatted after the initial write.

### Phase 3 — Test Results

All 13 integration tests + 4 unit tests pass with `-race`:

```
ok  wallet-transfer/internal/domain     1.009s
ok  wallet-transfer/tests/integration   2.471s
```

Zero `gofmt` diffs. Zero `go vet` warnings.

---

## What I Can Explain in the PR Discussion

I am prepared to walk through any of the following without notes:

- Why `UNIQUE(transfer_id, entry_type)` on `ledger_entries` is the right constraint for double-entry safety, and why a trigger alone wouldn't be sufficient
- Why the idempotency record must be updated in the **same transaction** as the transfer, not in a subsequent one, and what failure mode that prevents
- What happens step by step when two concurrent goroutines send `Transfer A→B` and `Transfer B→A` simultaneously, and why neither deadlocks nor double-spends
- Why `ORDER BY id FOR UPDATE` is the correct lock acquisition order and how swapping two wallets' lock order between two concurrent transactions creates the circular wait that causes a deadlock
- Why `NumericToCents` uses only integer arithmetic and string parsing, and what would go wrong if we used `float64` multiplication to convert `100.50` to cents (hint: `100.50 * 100 = 10049.999999999998` in IEEE 754)
- The `IN_PROGRESS` state in `idempotency_records` and what it means in practice — when it would be seen and what the correct client behaviour is
- The guarded `UPDATE transfers SET status='PROCESSED' WHERE id=$1 AND status='PENDING'` and why checking `RowsAffected()` is necessary (and what the database-level trigger adds on top)
- Any of the 13 integration tests: what it's testing, why that scenario matters, and what the expected invariant is

---

## Honest Assessment of the Output

**Where AI was clearly valuable:**
- Scaffolding the layered package structure quickly and consistently
- The `money.go` converter — getting the exact integer-only round-trip right without a decimal library
- Test coverage breadth — it covered all 13 scenarios from the spec systematically

**Where I added the most value:**
- Catching the `float64` JSON boundary bug before it shipped
- Catching the `pq.Array` / UUID type mismatch
- The overall design decisions (locking order, idempotency-in-same-transaction, in-flight 409) required genuine understanding to evaluate, not just acceptance
- Verifying the concurrency reasoning is actually correct, not just that the code compiles

**What this means for the PR discussion:**
I can explain every line. If something looks wrong or questionable, that's a good conversation to have — that's exactly the kind of review this submission is designed to support.
