## Summary

Implements the wallet transfer service described in `ASSIGNMENT.md`: a Go
HTTP API for wallet-to-wallet transfers with idempotent request handling
(exactly-once at the API level), a double-entry ledger, and row-lock-based
concurrency control that prevents double spending and negative balances
under concurrent load. Full design rationale is in
[`docs/design.md`](../docs/design.md); the API contract is in
[`docs/api.md`](../docs/api.md).

## AI disclosure

1. **Tool:** Claude Code (CLI) — Claude Opus 5 for the architecture and
   planning pass, Claude Sonnet 5 for implementation.
2. **General usage:** documentation-first — design in prose and commit it
   before writing code, implement layer by layer (domain → postgres →
   service → HTTP → integration tests → CI → docs), each layer building and
   its tests passing before moving on. Every commit in this PR's history is
   independently buildable.
3. **Prompts / transcript:** the complete build specification the
   implementation was written against is committed as
   [`prompt.md`](../prompt.md) at the repository root. Full disclosure,
   including the verbatim human prompts, is in
   [`docs/ai-usage.md`](../docs/ai-usage.md).

## Schema Design

Three tables: `wallets`, `transfers`, `ledger_entries` (plus
`schema_migrations`, managed by an embedded migrator). Full annotated SQL
and rationale: [`docs/design.md` §3](../docs/design.md#3-domain-model-and-schema).

Highlights:

- Money is `BIGINT` minor units, never floating point.
- `wallets.balance_minor` is the lockable, fast-read balance; `ledger_entries`
  is the append-only audit truth. They're kept in agreement structurally —
  `balance_after` is written from the same value that updates the balance,
  in the same transaction — and an integration test independently sums the
  ledger per wallet and asserts it matches the stored balance.
- Only `SYSTEM` wallets (the seeded `treasury_inr`) may hold a negative
  balance, which makes `SUM(balance_minor) = 0` across every wallet a real,
  tested global invariant — no money is created outside the ledger.
- Two triggers back the application logic: `transfers` rows are immutable
  except for the single legal `PENDING -> {PROCESSED,FAILED}` transition,
  and `ledger_entries` rejects `UPDATE`/`DELETE` outright.
- `UNIQUE (transfer_id, entry_type)` on `ledger_entries` guarantees at most
  one DEBIT and one CREDIT per transfer at the database level.

## Idempotency Strategy

`UNIQUE (idempotency_key)` on `transfers` — the transfer row *is* the
idempotency record; no separate `idempotency_records` table. Full rationale
for that choice, and when a separate table *would* be worth it:
[`docs/design.md` §6](../docs/design.md#6-idempotency-behaviour).

- **Detection:** a lock-free lookup by key first (the fast path for a key
  already seen), then `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING`
  inside the transaction — Postgres blocks a conflicting insert until the
  other transaction resolves, which is what makes concurrent duplicate
  requests safe with no extra "claimed" state.
- **Returning the original result:** a replay returns the identical HTTP
  status and body as the original, plus an `Idempotent-Replay: true`
  header.
- **Reuse with a different payload:** the request is fingerprinted
  (`sha256` of the canonical fields); a mismatched fingerprint on the same
  key is rejected with `409 IDEMPOTENCY_KEY_REUSE` rather than silently
  returning someone else's transfer.
- **Which failures are persisted:** validation/not-found/currency errors
  are rejected *before* the key is claimed (deterministic, so idempotent
  without storage); insufficient funds is a *committed* `FAILED` transfer
  (so a retry gets a stable answer forever); infrastructure errors roll
  back the whole transaction, leaving nothing — not even a `PENDING` row.
  Full table: [`docs/design.md` §8](../docs/design.md#8-failure-modes).

Tested at 25-way concurrency in `test/integration/idempotency_test.go`:
exactly one transfer row, one non-replayed response, balance moved exactly
once.

## Concurrency Strategy

Full rationale: [`docs/design.md` §7](../docs/design.md#7-concurrency-and-consistency).

- **Isolation:** READ COMMITTED (Postgres default) — sufficient because
  every value that feeds a decision is read under an explicit row lock in
  the same transaction that acts on it.
- **Lock type:** `SELECT ... FOR NO KEY UPDATE`, not `FOR UPDATE`. Inserting
  a transfer takes an implicit `FOR KEY SHARE` lock on both wallets for the
  foreign keys; `FOR UPDATE` conflicts with that in a way that can deadlock
  two concurrent transfers even with sorted lock acquisition. `FOR NO KEY
  UPDATE` doesn't conflict with `FOR KEY SHARE`, still conflicts with
  itself, and is exactly the strength a non-key-column update needs.
- **Lock ordering:** both wallets locked in ascending `id` order, in two
  separate statements (not one sorted multi-row lock — Postgres doesn't
  guarantee a single statement acquires locks in sorted order). This is
  what makes concurrent `A->B` and `B->A` transfers deadlock-free.
- **Three layers against negative balances:** the locked-balance check in
  the service, a guarded `UPDATE ... WHERE balance_minor + delta >= 0` in
  the repository, and a database `CHECK` constraint as the final backstop.
- **Retry:** the whole transaction is retried up to 3 times with jittered
  backoff on `serialization_failure`/`deadlock_detected`, safe because it's
  idempotent by construction.

Tested in `test/integration/concurrency_test.go`: 10 concurrent transfers
against a wallet funded for exactly 3 (asserts exactly 3 succeed, 7 fail,
balance never negative), and 60 concurrent bidirectional transfers (asserts
no request ever surfaces a 500/deadlock).

## How to Run

```bash
cp .env.example .env
make db-up
make run        # applies migrations, serves on :8080
```

See [`README.md`](../README.md) for a full `curl` walkthrough and
[`docs/api.md`](../docs/api.md) for the endpoint reference.

## How to Test

```bash
make test-unit   # no database required
make test        # full suite (unit + integration) with -race
```

`internal/domain`: pure unit tests (state machine, validation, fingerprint).
`internal/service`: unit tests against in-memory fakes (idempotency policy,
retry behavior). `internal/httpapi`: `httptest` against a fake service
(every status-code branch, malformed/oversized bodies). `internal/postgres`
and `test/integration`: real Postgres, including the concurrency tests
above and a global-invariant check run at the end of every integration
test.

## Tradeoffs / Assumptions

- Single service, single currency per transfer (no FX), no auth/rate
  limiting — out of scope for what this assignment is evaluating.
- `PENDING` is modeled but not reachable after commit today, because the
  whole lifecycle is one transaction; documented honestly in
  `docs/design.md` §8 along with the two-phase/outbox design that would
  make it reachable if settlement ever crossed a network boundary.
- Wallet balance is stored (not derived per-read from the ledger) —
  deliberate, see `docs/design.md` §3.4 and the alternatives in §12.
- No transfer reversal/refund flow.
- `GET /wallets/{id}`, `GET /wallets/{id}/entries`, `/metrics` are the
  assignment's optional enhancements; implemented at a basic but complete
  level, not gold-plated.
- The data-access layer uses GORM (connection pooling, transaction
  lifecycle, simple reads). The concurrency-critical statements — the
  `FOR NO KEY UPDATE` lock, the guarded balance `UPDATE`, the
  `ON CONFLICT DO NOTHING` idempotency claim — are still the exact raw SQL
  described above, issued through GORM's own escape hatches
  (`clause.Locking`, `.Raw()`, `clause.OnConflict`) rather than left to its
  query builder. Full rationale and what stayed vs. changed:
  `docs/design.md` §13.

## Checklist

- [x] Tests pass (`go test ./... -race -count=1 -p 1`, verified against a
      real Postgres instance)
- [x] Lint passes (`golangci-lint run ./...`, v1.64.8, verified under a
      matching Go 1.24 toolchain)
- [x] Format check passes (`gofmt -l .` empty)
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
