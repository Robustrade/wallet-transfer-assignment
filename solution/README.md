# Wallet Transfer Service

A wallet-to-wallet transfer service with idempotent, atomic, double-entry
ledger-backed transfers. Implements the brief in
[`../ASSIGNMENT.md`](../ASSIGNMENT.md).

## Architecture

```
cmd/server/main.go        entry point: wiring, HTTP server, graceful shutdown
internal/handler/http/    HTTP handlers, routing, request/response DTOs
internal/service/         business logic: validation, idempotency, orchestration
internal/repository/      persistence interfaces (postgres/ implements them)
internal/domain/          entities, state machine, sentinel errors
internal/db/              Postgres connection pool
migrations/               SQL schema
scripts/seed.sql          wallet seed data for manual testing (make seed)
```

Layering: **handler** does request/response mapping only; **service**
owns validation and the transfer workflow; **repository** does
persistence and nothing else; **domain** has no knowledge of HTTP or SQL.
The service depends on repository *interfaces*
(`internal/repository/interfaces.go`), not the concrete Postgres types,
so it's testable without a database (see `transfer_service_test.go`'s
hand-rolled fakes).

Those interfaces are deliberately **consumer-shaped**: they list only the
methods `TransferService` actually calls, so nothing exists to satisfy a
layer diagram. Postgres-specific helpers (SQLSTATE checks) live in
`repository/postgres` beside their only caller rather than in a general
`utils` package — that package was removed once it became clear it would
only ever hold two functions used from one file.

### The transfer write path

```
POST /transfers
   │
   ▼
TransferHandler.Create          (decode JSON, reject trailing data, map errors → status)
   │
   ▼
TransferService.CreateTransfer
   │
   ├─ fingerprint the request         sha256(from ∥ to ∥ amount)
   ├─ transfers.GetByIdempotencyKey   has this key been used?
   │     ├─ fingerprint differs  → refuse (409)
   │     ├─ still PENDING        → resume it, then return its terminal result
   │     └─ terminal             → replay that outcome, error included
   ├─ validate                        amount > 0, wallets differ
   ├─ wallets.Get × 2                 confirm both wallets exist
   ├─ transfers.Create                insert the transfer row as PENDING
   └─ executor.Execute                atomic debit + credit + ledger + PROCESSED
```

The idempotency lookup runs **before** validation deliberately: a key that
already has a transfer must answer with that transfer, not with a fresh
validation error about the retry's payload.

`TransferExecutor.Execute` (`internal/repository/postgres/transfer_executor.go`)
is the one place a database transaction spans more than one table. It's a
single purpose-built method, not a generic reusable unit-of-work —
transfers are the only operation in this service that need a
multi-statement transaction, so there's no second caller to justify that
abstraction.

The debit is a single conditional `UPDATE`:

```sql
UPDATE wallets SET balance = balance - $1, updated_at = now()
WHERE id = $2 AND balance >= $1
```

Postgres takes the row lock as part of the `UPDATE` itself, so the
balance check and the decrement happen atomically — no separate
`SELECT ... FOR UPDATE`, no window for a concurrent transfer to read a
stale balance. Mechanically, this relies on EvalPlanQual: when the
`UPDATE` hits a row a concurrent transaction is modifying, it waits, then
re-evaluates `WHERE balance >= $1` against the new row version once that
transaction commits — under this transaction's default READ COMMITTED
isolation. (At REPEATABLE READ the same statement would raise a
serialization failure, SQLSTATE 40001, instead of quietly returning 0
rows — worth knowing if the isolation level ever changes.)

**This does *not* mean no lock-ordering rule is needed** — an earlier
version of this README claimed that, and it was wrong. An `UPDATE` holds
its row lock until commit exactly like `SELECT ... FOR UPDATE` would: a
transfer A→B locks A then wants B, while a concurrent transfer B→A locks
B then wants A — a real deadlock cycle, reproduced against a live
Postgres (`SQLSTATE 40P01`) before this was fixed. `Execute` now runs the
two wallet `UPDATE`s in ascending wallet-id order regardless of transfer
direction, which breaks the cycle: both directions now contend for the
same wallet first instead of forming a circular wait.
`TestTransferExecutor_Execute_OppositeDirectionDeadlock` fires transfers
concurrently in both directions between the same two wallets and would
fail (with that exact SQLSTATE) if this regressed.

Three more things `Execute` guards against, all cheap and worth having.

The state transition is a compare-and-swap (`WHERE state = 'PENDING'`),
not an unconditional write, so a second call against an already-`PROCESSED`
transfer fails loudly instead of re-applying the debit/credit. That CAS is
also what makes resuming a stranded transfer safe: if two callers execute
the same transfer at once, one commits and the other's CAS matches nothing,
rolling its whole transaction back. `TransferState.CanTransitionTo` states
the rule in the domain — only `PENDING` moves, and only to a terminal state
— so no repository call can walk a transfer backwards into `PENDING` and make
it eligible for resuming again.

Both ledger entries are checked against the transfer before the transaction
opens — type, transfer id, wallet ids *and* amounts. The balance updates are
driven by `transfer`, but the ledger rows are written from the entries the
caller passes in, so validating only the amounts would still let a caller bug
move exactly the right money and record a reversed or cross-wallet pair
against it.

The deferred rollback runs on `context.WithoutCancel(ctx)` so it still
reaches Postgres on a dead context, but wrapped in a short timeout: on its
own `WithoutCancel` also strips the deadline, and a stalled connection would
then hold the rollback — and its pool slot — open indefinitely.

## API

### `POST /transfers`

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Response (201 on the first call, and on a replay of a *processed* transfer —
see the status table below for replays of failed or in-flight ones):

```json
{
  "id": "90c0261c-327a-4476-91b1-afe6dbec4615",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "state": "PROCESSED"
}
```

| Case                                   | Status |
|-----------------------------------------|--------|
| `GET /healthz` (process is up)          | 200    |
| Success                                 | 201    |
| Replayed duplicate of a *processed* transfer | 201, identical body to the original |
| Replayed duplicate of a *failed* transfer | 422, identical to the original response |
| Replay of a PENDING transfer              | resumes it and returns the terminal result (201 or 422) |
| Same `idempotencyKey`, different payload      | 409 |
| `amount <= 0` / self-transfer            | 422    |
| Insufficient funds                       | 422    |
| Unknown wallet                           | 404    |
| Malformed JSON body                      | 400    |

### Idempotency contract

**A replay reproduces the original outcome, error included.** An earlier
version returned `201 {"state":"FAILED"}` when a failed transfer's key was
retried, which told a retrying client the money had moved when it never
had. Only a `PROCESSED` transfer is ever reported as a success.

**A key is bound to the request it was first used with.** `transfers` stores
`request_hash`, a SHA-256 of `from ∥ to ∥ amount` (NUL-separated so two
different requests cannot join to the same string). Every replay recomputes
it and compares. Without this, the key is just a string the server has seen
before, so reusing it for a *different* transfer would silently return the
first one's result — the caller would be told their transfer succeeded when
nothing they asked for ever happened. A mismatch is refused with `409`.

**A transfer that was created but never executed gets finished, not
stranded.** The `PENDING` row is committed before `Execute` runs, so a crash
in that gap leaves a row with no outcome. A retry of the same key resumes it
rather than reporting it permanently in progress. If the original caller is
in fact still running, the state CAS above means exactly one of them commits.

**A failed transfer records its failure atomically.** The wallet updates run
inside a savepoint, so when the debit is declined `Execute` rolls back to it
— undoing the credit if that leg sorted first — then marks the transfer
`FAILED` and commits, all in the transaction that declined to move the money.
An earlier version wrote `FAILED` in a *second* statement after `Execute`
returned; a crash or a cancelled request in that gap left the row `PENDING`,
and a later retry would then execute the transfer once funds had arrived
instead of replaying the original failure.

### `GET /healthz`

Returns `200 {"status":"ok"}` while the process is up and serving. It
deliberately does not check the database: a dependency being down is not
a reason for an orchestrator to restart an otherwise healthy process.
Because of that it is a liveness signal only — it will keep returning 200
with an unreachable database. What catches that case here is startup (see
Deploy), not this endpoint.

There is currently no endpoint to create a wallet or read a balance —
see Known Limitations below.

## How to Build

```bash
go build ./...
```

## How to Run

There is no dotenv loader — the server reads plain environment variables —
so `.env.example` is a reference to copy values from, not a file that is
read at startup. Export them, or pass them inline as below.

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable"

make up             # starts Postgres on localhost:5432, waits for healthy
make migrate-up      # applies migrations/0001_init.up.sql
make seed            # seeds wallet_1 (1000), wallet_2 (500), wallet_3 (0) — scripts/seed.sql
go run ./cmd/server
```

`make seed` is re-runnable and is a true reset: it clears prior transfers,
ledger rows and idempotency keys for those three wallets before restoring
their balances, so state can't disagree with the ledger and an old key can't
replay. If it finds a transfer with one leg *outside* the seeded set it
refuses rather than corrupting that wallet's balance. **Stop the service
first** — nothing coordinates the reset with a live `TransferExecutor`, so a
transfer committing alongside it could land on top of the reset state.
There's no wallet-creation API yet (see Known Limitations), so this script is
the only way to get a wallet to test against.

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"key-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

## How to Test

```bash
go test ./...                    # unit tests only; Postgres tests self-skip
```

**68 tests** (43 top-level, 25 subtests). Two kinds:

- **Unit tests** — no database, using hand-rolled fakes.
  `internal/service/transfer_service_test.go` covers validation, every
  replay path (processed, failed, still-pending, resumed, and resume losing
  the race), payload-mismatch refusal, the same-key race, and that the
  `FAILED` transition survives a client disconnect.
  `internal/handler/http/transfer_handler_test.go` covers the transport
  contract: every error→status mapping, that a failed transfer is never
  reported as a 201, that internal error details aren't leaked to the
  client, and that malformed or trailing-data bodies never reach the service.
- **Integration tests** (`internal/repository/postgres/integration_test.go`,
  `internal/service/integration_test.go`) — need a real Postgres with the
  migration applied; they skip themselves (not fail) if `DATABASE_URL`
  isn't set. Cover repository behaviour, the guarded state transition,
  ledger balancing, the executor's success / insufficient-funds /
  double-execution / entry-validation / cancelled-context paths, resuming a
  transfer stranded in `PENDING`, payload-mismatch refusal, and four
  concurrency tests — simultaneous debits on one wallet,
  opposite-direction transfers (the deadlock regression), a three-way
  circular cycle, and 20 concurrent `CreateTransfer` calls sharing one
  idempotency key. These are what actually prove the concurrency-safety
  claim rather than just compiling it:

```bash
docker compose up -d
docker compose exec -T postgres psql -U postgres -d wallet_transfer -f - < migrations/0001_init.up.sql
DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable" go test ./...
```

The unit tests need no database; the integration tests were run against a
real local Postgres as part of building this, not written and left
unverified.

Every concurrency test releases its workers from a **two-phase barrier** —
each worker finishes its setup and signals ready, and only once all of them
are parked is the gate opened. An earlier version closed the gate straight
after launching the goroutines, which does not guarantee they have reached
it; the workers could then run one after another and a read-then-write
implementation would pass. Several of these were also mutation-tested:
reverting the fix they guard makes them fail.

```bash
make lint        # golangci-lint, 13 linters (.golangci.yml)
make fmt-check   # fails if anything is not gofmt'd
```

## Observability

Structured (`log/slog`, JSON in production) logs, not `log.Printf`:

- One line per HTTP request (`internal/handler/http/middleware.go`):
  method, path, status, duration.
- One line per meaningful transfer event
  (`internal/service/transfer_service.go`): created, processed, failed
  (insufficient funds), idempotency replay, idempotency race lost —
  each tagged with `transfer_id` and `idempotency_key` so one transfer's
  whole lifecycle can be traced through the logs.

A resumed transfer and a key reused with a different payload each log a
line too, so both are visible in operation rather than only in tests.

Not implemented: metrics/counters (e.g. `PROCESSED` vs `FAILED` rate), and
there is no sweeper that finds a `PENDING` row nobody ever retries — see
Known Limitations.

## Deploy

```bash
docker build -t wallet-transfer-service .
docker run -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/wallet_transfer?sslmode=require" \
  wallet-transfer-service
```

Multi-stage build (`Dockerfile`): compiles a static binary in a
`golang:1.23-alpine` stage, ships it in a bare `alpine` image running as
a non-root user. Configuration is entirely via environment variables —
`DATABASE_URL` and `HTTP_ADDR` (default `:8080`) — no config files, so
it drops into whatever secrets/config mechanism the target platform
already has. This was built and run end-to-end (`docker build`, then the
container hitting a `docker compose` Postgres) as part of writing it.

Startup fails fast on an unreachable database. `pgxpool` dials lazily, so
without an explicit check the process would bind its port, log
`listening`, and then return 500 for every request with nothing behind
it. `db.Connect` pings (capped at 5s) and returns the error, which
`run()` turns into a non-zero exit — so a container that comes up with no
database crashes instead of pretending to be healthy.

The server sets `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and
`IdleTimeout`, and the handler caps the request body at 64 KiB. Without a
read timeout a client can hold a connection open indefinitely by trickling
the body a byte at a time.

`GET /healthz` gives an orchestrator a liveness signal. Kept deliberately
simple for the scope of this assignment: a readiness probe that reports
whether the database is reachable would be the next step, and is what you
would actually wire a load balancer to.

Not included, and worth doing before this went anywhere near real
production traffic: that readiness probe, and running the migration as an
explicit release step rather than the manual `psql` invocation above.

## Pros and Cons of the Key Decisions

**Conditional `UPDATE` instead of `SELECT ... FOR UPDATE` + application
check.**
✅ Simpler within a single transfer: one statement instead of
read-then-decide-then-write.
✅ Correct under same-direction concurrency by construction — verified by
`TestTransferExecutor_Execute_ConcurrentDebits`.
❌ Still needs an explicit lock-ordering rule across the *two* wallets a
transfer touches — an `UPDATE` holds its row lock until commit exactly
like `SELECT ... FOR UPDATE` would, so this doesn't avoid the
opposite-direction deadlock the way an earlier version of this doc
claimed. See the write-path section above and
`TestTransferExecutor_Execute_OppositeDirectionDeadlock`.
❌ The insufficient-funds check is now inside the SQL `WHERE` clause
rather than visible as service-layer business logic — a reviewer looking
only at `internal/service` wouldn't find it there.

**`TransferExecutor` (one purpose-built method) instead of a generic
`UnitOfWork`.**
✅ Less code, more readable — the entire effect of a transfer is visible
in one function.
✅ Matches how ledger-writing systems often prefer explicit,
one-operation-per-function code for auditability.
❌ Doesn't reuse the wallet repository's own methods (it duplicates a few
lines of SQL instead) — the trade-off point is documented in
`transfer_executor.go`'s comment. There is consequently no ledger
repository at all: `TransferExecutor` is the only thing that touches
`ledger_entries`, and the integration tests read those rows with direct
SQL rather than through persistence code that would exist only for them.
❌ If a second multi-table atomic operation shows up (a refund, say),
this pattern doesn't extend cleanly — that's the point at which a shared
transaction helper would earn its cost.

**Mixed id column types: `UUID` for server-generated ids, `TEXT` for
caller-supplied ones.**
`transfers.id`, `ledger_entries.id` and `ledger_entries.transfer_id` are
`UUID`; `wallets.id` and `idempotency_key` are `TEXT`.
✅ The service is the only writer of the first group — they always come
from `uuid.NewString()` — so the column can be the narrow type: 16 bytes
instead of 36-plus-header, paid again in the primary key, the
`idx_ledger_entries_transfer_id` index, and the foreign key. `ledger_entries`
is the table that grows without bound, so that is where it compounds.
✅ The database rejects a malformed id outright (`SQLSTATE 22P02`) rather
than storing it — correctness enforced by the schema, not by convention.
✅ `TEXT` is still right for the other two: wallet ids are supplied by the
caller and are not UUIDs (`wallet_1` in the brief), and `idempotency_key`
is an arbitrary client-chosen string.
❌ The schema is no longer uniform — two id types means a reader has to
know which is which, and it forecloses the SQLite fallback the assignment
lists as acceptable, since SQLite has no `UUID` type.
❌ No Go-side change was needed (pgx encodes a Go `string` into a `uuid`
parameter and scans it back), so the type discipline lives only in the
database — `domain.Transfer.ID` is still a `string` and would accept
anything if some future code path set it by hand.

**Idempotency via a `UNIQUE` constraint on `transfers.idempotency_key`,
not a separate `idempotency_records` table.**
✅ One fewer table; the constraint is what actually prevents the
duplicate, so there's no gap between "checked" and "enforced."
✅ The key is bound to its payload by `request_hash`, so it cannot answer
for a different request.
❌ The fingerprint is a second thing to keep in step with the payload: add a
field to the request and forget to hash it, and two different transfers
become indistinguishable again. For a payload this small, comparing the
three stored columns directly would be equivalent and one concept fewer —
the hash is chosen because it stays a single column as the payload grows.

**Wallet balance as a stored, updated column (not derived from summing
`ledger_entries`).**
✅ Reading a balance is O(1), and the conditional `UPDATE` technique
depends on it being a real column to check against.
❌ Balance and the ledger are two sources of truth that could drift if a
future code path updates one without the other — nothing currently
reconciles them against each other.

## Known Limitations

- **A `PENDING` row that nobody retries is never resolved.** A retry of the
  same key resumes it, so a client that retries recovers on its own — but if
  the caller crashes and never comes back, the row sits there. There is no
  sweeper to find and finalise it, and until something does, the funds are
  neither moved nor released.
- **The fingerprint covers the request, not the client.** Two different
  callers picking the same key for genuinely different transfers get a 409,
  which is correct but opaque; scoping keys per API client would be the real
  fix.
- **No wallet-creation or balance-read API** — wallets only exist via
  `scripts/seed.sql` (`make seed`). Listed as optional in the assignment,
  but worth calling out since there's no way to create an arbitrary
  wallet through the running service itself.
- **On insufficient funds, the client doesn't get the transfer's id** —
  just `{"error": "insufficient funds"}` with 422, even though the
  `FAILED` transfer row does exist. Revisit if a client needs to look up
  a failed attempt later.
- **`FAILED` is terminal for a given idempotency key.** Once a key's
  transfer fails, every retry of that key replays the 422 — even after the
  wallet is funded. That's the correct reading of exactly-once ("return
  the original result"), but it does mean a client retrying after topping
  up must use a *new* key. Worth stating explicitly in a client-facing
  API doc.
- **A malformed transfer id would surface as a 500, not a 404.** Now that
  `transfers.id` is `UUID`, querying it with a non-UUID string raises
  `SQLSTATE 22P02` rather than returning no rows. Nothing is exposed today
  — no endpoint accepts a transfer id — but whoever adds
  `GET /transfers/{id}` needs to map that code to a 400/404 instead of
  letting it fall through to the generic internal-error branch.
- **No DB-level unique constraint on `ledger_entries (transfer_id, wallet_id)`.**
  The application-level guards (the transfer-state CAS in
  `TransferExecutor.Execute`, plus the fact that nothing currently calls
  `Execute` twice for the same transfer) should prevent a duplicate pair
  of ledger rows today, but there's no constraint enforcing it at the
  database level as a second line of defense.
- **Ledger `amount` is unsigned; direction comes from `type` (DEBIT/CREDIT)
  alone**, and `now()` in the migrations' `updated_at` columns is
  transaction-start time (`transaction_timestamp()`), not per-statement
  time (`clock_timestamp()`) — both deliberate for this schema's scope,
  but worth a second look if requirements around signed ledger amounts or
  sub-transaction timestamp precision ever show up.
