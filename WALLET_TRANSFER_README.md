# Wallet Transfer Service

A small HTTP service for wallet-to-wallet transfers with idempotent
requests, a double-entry ledger, and concurrency-safe balance updates.

> **Note on verification:** this was written without a Go toolchain or
> network access in the environment it was authored in. Before you rely on
> it, run:
> ```
> go mod tidy
> go build ./...
> go test ./... -race
> ```
> The logic has been traced carefully by hand and the tests describe the
> intended behavior precisely, but a compiler pass hasn't happened yet.

## Running it

```bash
createdb wallets
psql wallets < db/schema.sql
export DATABASE_URL="postgres://localhost:5432/wallets?sslmode=disable"
go run ./cmd/server
```

```bash
curl -X POST localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Seed wallets directly via SQL for now (`INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000)`)
— a wallet-creation endpoint was out of scope per the assignment's "not full
feature completeness" framing, but it'd be a five-minute addition following
the same handler → service → repository pattern as transfers.

## Architecture

```
cmd/server/main.go              wiring: config, DB connection, routes
internal/handler/               HTTP transport: decode, call service, encode
internal/service/               business logic: validation, idempotency, workflow
internal/repository/            persistence contract (interfaces only)
internal/repository/postgres/   production implementation (database/sql + pgx)
internal/repository/memory/     in-memory fake, used only by tests
internal/domain/                entities, state machine, sentinel errors
internal/idgen/                 dependency-free random ID generation
db/schema.sql                   Postgres schema
```

The service layer depends only on the `repository.TxStore` / `repository.Tx`
interfaces, never on `database/sql` directly. That's what makes the business
logic (idempotency replay, insufficient-funds handling, the debit/credit/
ledger sequence) testable in milliseconds against `memory.Store`, with zero
mocking framework and no test database to stand up.

## Database schema

Four tables: `wallets`, `transfers`, `ledger_entries`, `idempotency_records`.
See `db/schema.sql` for full DDL with comments. Highlights:

- `wallets.balance` is a non-negative `BIGINT` in minor units (cents), never
  a float — avoids rounding drift across many transfers. The `CHECK
  (balance >= 0)` is a last line of defense; the app never issues an update
  that could trip it because the balance is checked under a lock before
  every debit.
- `transfers.state` is constrained to the three allowed values and
  `from_wallet_id <> to_wallet_id` is enforced at the DB level too, not just
  in application code.
- `ledger_entries` has a foreign key to `transfers`, so orphaned entries are
  impossible.
- `idempotency_records.idempotency_key` is the primary key — this single
  constraint is what makes concurrent duplicate requests safe (see below).

## Idempotency strategy

Every request with an `idempotencyKey` follows this sequence, all inside one
database transaction:

1. **Claim the key.** `INSERT INTO idempotency_records (idempotency_key, ...)`.
   Only one concurrent request can ever win this insert — everyone else gets
   a `unique_violation`.
2. **Losers read the winner's record.**
   - If the stored `request_hash` (a hash of `fromWalletId` + `toWalletId` +
     `amount`) doesn't match the incoming request, we return `409
     idempotencyKey was reused with a different request payload` — this is a
     client bug, not a legitimate retry, and silently picking one
     interpretation would be the wrong call.
   - If the stored record is `COMPLETED` or `FAILED`, we return the exact
     cached `TransferResult` — a byte-for-byte replay, no re-execution.
   - If it's still `IN_PROGRESS` (a genuinely concurrent duplicate, not a
     retry-after-response), we return `409` asking the caller to retry
     shortly, rather than blocking the connection open.
3. **The winner executes the transfer** (see below) and then writes the
   final `TransferResult` back onto the same idempotency row before
   committing.

**Why one DB transaction end-to-end matters:** if step 3 fails for an
*unexpected* reason (a dropped connection, a bug), the whole transaction —
including the idempotency claim from step 1 — rolls back. The key is
released, and the client's retry gets a fresh attempt instead of being
stuck replaying a phantom failure. Deterministic business failures (like
insufficient funds) are the opposite case: those get *committed* as a
`FAILED` result, specifically so a retry returns the same answer instantly
instead of re-locking wallets and re-doing work whose outcome can't change.

## Concurrency handling

Two wallets are locked with `SELECT ... FOR UPDATE` for the duration of the
transaction, always in **ascending wallet-ID order** regardless of which is
`from` and which is `to`. This ordering is the whole deadlock-prevention
strategy: if transfer A moves money `wallet_1 → wallet_2` and transfer B
moves money `wallet_2 → wallet_1` at the same instant, both acquire locks in
the same order (`wallet_1` then `wallet_2`), so one simply waits for the
other instead of each holding a lock the other needs.

Once both locks are held, the balance check and the debit happen inside the
same transaction with no gap where another writer could interleave — that's
what turns "check balance, then debit" from a race condition into an atomic
operation. `READ COMMITTED` is sufficient here because correctness comes
from the explicit row locks, not from the isolation level.

This is exercised directly in
`internal/service/concurrency_test.go`: 20 concurrent transfers of 50 units
each fire against a wallet with a balance of 500, and the test asserts
*exactly* 10 succeed, the balance lands at exactly 0, and the ledger has
exactly 20 entries — proving no double-spend and no dropped transfer.

## Testing

```
internal/service/transfer_service_test.go   validation, success path, insufficient funds,
                                             idempotent replay, ledger correctness
internal/service/concurrency_test.go        concurrent debits can't overdraw a wallet;
                                             concurrent duplicate keys produce one transfer
internal/handler/http_handler_test.go       status codes, JSON shape, idempotent replay over HTTP
```

All service/handler tests run against `memory.Store`, so `go test ./...`
needs no database and no `-tags` — the concurrency and idempotency
guarantees are properties of the service logic itself, provable without a
real Postgres instance. The Postgres implementation adds the actual
row-locking mechanism on top; it isn't unit-tested here for lack of a
database in this environment, but its query logic mirrors the interface
contract the memory store already proves correct.

## Assumptions & tradeoffs

- **No wallet-creation endpoint.** Out of scope per the brief; wallets are
  seeded directly in SQL for this exercise.
- **In-flight duplicates return 409, not a blocking wait.** Waiting for the
  in-progress request to finish and then replaying its result would be
  friendlier, but adds real complexity (polling, timeouts) for a case that's
  narrow in practice (true concurrent duplicates, not sequential retries).
  Documented as the simpler of two reasonable choices.
- **Unknown wallet aborts the whole transaction (idempotency key released),
  rather than being committed as a FAILED transfer.** This is arguably
  inconsistent with how insufficient-funds is handled — a case could be made
  either way. I chose "release the key" because a wallet that doesn't exist
  yet is more likely to be a client-side mistake worth surfacing fresh on
  retry, whereas insufficient funds is a legitimate steady-state outcome.
- **Money amounts are `int64` minor units supplied by the caller** — no
  currency field, no multi-currency handling. Adding one would mean
  validating both wallets share a currency before any locking happens.
- **SQLite wasn't implemented** despite being an "acceptable alternative" in
  the brief — the `repository.TxStore` interface makes swapping the driver
  mechanical (a new package implementing the same interface with SQLite's
  `BEGIN IMMEDIATE` in place of `FOR UPDATE`), so Postgres was picked as the
  more production-representative option to spend the time budget on.
