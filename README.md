# Wallet Transfer Service

A wallet transfer service with idempotent request handling, a double-entry
ledger, and safe concurrent execution.

## Running

No database required for a quick run - defaults to an in-memory store:

```
go run ./main
```

Against Postgres:

```
docker compose up -d
$env:DATABASE_URL = "postgres://wallet:wallet@localhost:5433/wallet_transfer?sslmode=disable"
go run ./main
```

`docker-compose.yml` applies `migrations/0001_init.sql` automatically on
first start. `HTTP_ADDR` overrides the listen address (default `:8080`).

Run the tests:

```
go test ./...
```

`internal/service/transfer_service_test.go` includes concurrency tests:
many goroutines racing to overdraw one wallet, and many goroutines racing
on the same idempotency key.

## API

- `POST /wallets` - `{"id": "wallet_1", "initialBalance": 1000}`
- `GET /wallets/{id}` - `{"id": "wallet_1", "balance": 1000}`
- `POST /transfers` - `{"idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100}`
- `GET /transfers/{id}` - transfer plus its ledger entries

`amount` and `balance` are integers in the smallest currency unit, never floats, to avoid rounding drift.

## Architecture

```
handler/     transport: decode JSON, call a service method, encode the result
service/     business logic: idempotency, transfer workflow, state machine
repository/  persistence interfaces + postgres and memory implementations
domain/      entities, state transitions, validation
```

Handlers never touch SQL; the service never imports `net/http` or
`database/sql`. `repository.Store` is the seam between service and
persistence - `ExecTx` runs a closure inside one atomic unit of work, and
`postgres.Store` / `memory.Store` are interchangeable implementations of
it (selected in `main/main.go` by whether `DATABASE_URL` is set).

## Guarantees and how they're implemented

**Idempotency.** `idempotency_records` is keyed by `idempotency_key`.
Claiming a key is `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING`
inside the transfer's transaction - the database guarantees only one
concurrent request ever wins that insert, so exactly one execution of the
transfer happens per key, even if duplicate requests race in at the same
instant. Every other caller (retries, races, and a fast path that checks
before opening a transaction at all) reads back the same stored result
instead of re-executing anything. Reusing a key with a different request
body is rejected as a conflict (`422`) rather than silently returning the
old result for a different request.

**Double-entry ledger.** `service.executeTransfer`
([transfer_service.go](internal/service/transfer_service.go)) writes the
DEBIT and CREDIT rows for a transfer in a single `INSERT` inside the same
transaction that updates both wallet balances, so the pair is always
written together and the ledger always nets to zero per transfer. A
`FAILED` transfer (e.g. insufficient funds) writes no ledger entries and
touches no balances.

**Balances.** Stored on the `wallets` row (not derived from the ledger on
every read) and updated atomically alongside the ledger write in the same
transaction, so a balance and its ledger entries can never disagree.

**Transfer state machine.** `PENDING -> PROCESSED` and `PENDING ->
FAILED` are the only legal edges (`domain.CanTransition`). A transfer is
inserted `PENDING` and moved to its terminal state inside the same
transaction that decides the outcome, so no caller ever observes a
transfer stuck mid-transition, and a terminal state is never revisited by
a retry - the idempotency record short-circuits retries straight to the
stored result.

**Concurrency.** Two wallet locks are taken with `SELECT ... FOR UPDATE`
inside the transfer's transaction, in a fixed order (lowest wallet ID
first) regardless of transfer direction, before the balance check. This
rules out both failure modes in the spec's example case:

- *Two transfers debiting the same wallet at once*: both lock statements
  target the same row, so the second transfer blocks until the first
  commits (or rolls back) and sees the up-to-date balance - no
  overdrafts, no lost updates.
- *Two transfers between the same wallet pair in opposite directions*:
  both lock the lower ID first, so neither can hold one lock while
  waiting on the other - no deadlock.

READ COMMITTED (Postgres's default) is enough here because correctness
comes from the explicit row locks, not from the isolation level - this
avoids the serialization-failure retry loops `SERIALIZABLE` would need
for the same guarantee. See the doc comment in
[repository/postgres/store.go](internal/repository/postgres/store.go)
for the full reasoning.

The in-memory store (`internal/repository/memory`, used for tests and
the no-database quickstart) gets the same guarantees more bluntly: one
mutex serializes every unit of work, which is equivalent to row locking
for a store with no concurrent writers to interleave in the first place.

## Schema

See [migrations/0001_init.sql](migrations/0001_init.sql):
`wallets`, `transfers`, `ledger_entries`, `idempotency_records`.
