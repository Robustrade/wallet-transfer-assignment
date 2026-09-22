# Design Notes

This document is the documentation-first artifact for the wallet transfer
service: problem framing, contract, and the key design decisions, written
before/alongside the implementation in `cmd/` and `internal/`.

## Problem statement

Support wallet-to-wallet transfers with idempotent request handling,
double-entry ledger recording, correct balance tracking under concurrency,
and safe state transitions — see [`ASSIGNMENT.md`](../ASSIGNMENT.md) for the
full brief.

## API contract

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Responses:

| Status | Meaning |
|---|---|
| 201 | Transfer processed or recorded as a terminal business failure (e.g. insufficient funds). The response body's `state` field distinguishes `PROCESSED` from `FAILED`. |
| 400 | Invalid request (same wallet, non-positive amount, missing idempotencyKey). Not consumed as an idempotency key — safe to retry after fixing the request. |
| 404 | `fromWalletId` or `toWalletId` does not exist. Not consumed as an idempotency key. |
| 409 | `idempotency_key_conflict` — the key was already used with a different payload. `idempotency_in_progress` — a request with this key is still being processed; retry shortly (see `Retry-After` header). |

A replayed response is identical to the original and carries an
`Idempotent-Replayed: true` header for observability.

### Other endpoints

- `GET /transfers/{id}` — fetch a transfer and its ledger entries.
- `POST /wallets` — provision a wallet with an opening balance (test/demo
  convenience; not specified by the assignment but needed to exercise it).
- `GET /wallets/{id}/balance` — current wallet balance.
- `GET /wallets/{id}/transfers` — transfer history for a wallet.
- `GET /healthz` — liveness probe.

## Schema design

See [`internal/migrations/sql/0001_init.sql`](../internal/migrations/sql/0001_init.sql).

- `wallets.balance` is a **maintained** balance (updated in the same
  transaction as ledger writes), not derived by aggregating the ledger on
  every read. This keeps balance reads O(1) instead of O(n) ledger rows,
  at the cost of needing the two writes to stay atomic — guaranteed by
  doing both inside one DB transaction.
- `transfers.idempotency_key` is `UNIQUE`, so the database itself is a
  final backstop against duplicate transfers even if the application-level
  idempotency claim (below) were ever bypassed.
- `transfers` has a `CHECK (from_wallet_id <> to_wallet_id)` and
  `CHECK (amount > 0)` — invalid states can't be persisted even if
  application code has a bug.
- `ledger_entries.transfer_id` is a `NOT NULL FOREIGN KEY` — a ledger entry
  can never exist without a transfer, and `entry_type` is constrained to
  `DEBIT`/`CREDIT`.
- `idempotency_records` stores the claim: `key` (primary key), a
  `request_hash` fingerprint of the business fields, and the eventual
  `transfer_id` / `response_status` / `response_body` / `completed_at`,
  which stay `NULL` until the original request finishes. That's what lets a
  concurrent duplicate tell "replay this" apart from "still in flight."
- Indexes: `transfers(from_wallet_id, created_at)` /
  `(to_wallet_id, created_at)` for transfer history queries, and
  `ledger_entries(wallet_id, created_at)` / `(transfer_id)`.

## Idempotency strategy

1. The client-supplied `idempotencyKey` is claimed via
   `idempotency_records` **before** any wallet mutation, inside the same
   transaction as the rest of the transfer
   (`internal/repository/postgres/idempotency_repo.go`):
   - `INSERT ... ON CONFLICT (key) DO NOTHING RETURNING key` — if a row is
     returned, this request owns the key and proceeds.
   - Otherwise, `SELECT ... FOR UPDATE` the existing row. If it's already
     `completed_at IS NOT NULL`, its stored response is replayed verbatim.
     If not yet completed, the row lock means we're blocked behind the
     in-flight original transaction; once it commits/rolls back we either
     see the completed row (replay it) or, if the original rolled back, we
     retry the claim (bounded to a few attempts).
   - This makes concurrent duplicate delivery of the exact same request
     race-free without any apart from standard row locking.
2. `request_hash` is `sha256(fromWalletId|toWalletId|amount)`. Reusing a
   key with a materially different payload returns
   `409 idempotency_key_conflict` instead of silently doing the wrong
   thing.
3. **What consumes the key vs. what doesn't:** validation failures that
   happen before a transfer is durably recorded (unknown wallet, same
   wallet, non-positive amount) roll back the whole transaction, including
   the idempotency claim itself — so the key is never consumed and the
   client can fix the request and retry with the same key. Insufficient
   funds, by contrast, is treated as a legitimate terminal outcome: it's
   recorded as a `FAILED` transfer (no ledger entries) and the key **is**
   consumed, because retrying an unchanged request would fail identically.

## Concurrency strategy

- Every transfer runs inside a single Postgres transaction
  (`internal/repository/postgres/postgres.go: UnitOfWork.Execute`).
- Both wallets are fetched with `SELECT ... FOR UPDATE`, always in
  **ascending wallet-ID order** regardless of transfer direction
  (`internal/service/transfer_service.go: lockWalletsInOrder`). Two
  concurrent transfers between the same pair of wallets — even in opposite
  directions — always acquire row locks in the same order, which prevents
  the classic A-locks-1-then-2 / B-locks-2-then-1 deadlock.
- Balance reads and writes happen while holding that lock, so there's no
  read-then-write race: a second transaction touching either wallet blocks
  until the first commits, then sees the fully updated balance.
- This is a **pessimistic locking** strategy (explicit row locks) rather
  than optimistic concurrency (version column + compare-and-swap retries).
  It was chosen because financial transfers are typically low-contention
  per wallet pair but must never silently retry-and-drop a client request;
  blocking briefly under contention is preferable to surfacing spurious
  conflicts to the caller.
- Verified by `internal/service/concurrency_test.go` and
  `internal/repository/postgres/postgres_integration_test.go` against real
  PostgreSQL row locks and transactions.

## Layering

```
cmd/server        entrypoint: config, wiring, HTTP server, graceful shutdown
internal/controller transport: routing, JSON (de)serialization, status mapping
internal/service   business logic: transfer workflow, idempotency, locking order
internal/domain    entities, state machine, validation, sentinel errors
internal/repository            interfaces (WalletRepository, TransferRepository, ...)
internal/repository/postgres   pgx-based implementation + UnitOfWork
internal/migrations            embedded SQL migrations, applied on startup
```

The service layer depends only on `internal/repository`'s interfaces. The
application uses `internal/repository/postgres`, while service and HTTP tests
use repository mocks from `internal/testutil`.

## Testing

- `internal/domain`: state machine and validation unit tests.
- `internal/service`: business logic against repository mocks —
  success, insufficient funds, idempotent replay, idempotency conflict,
  wallet-not-found (key not consumed), concurrent debits, concurrent
  duplicate requests.
- `internal/controller`: end-to-end HTTP tests (`httptest`) covering the full
  request/response contract, including the idempotent-replay header and
  error status mapping.
- `internal/repository/postgres`: concurrency and idempotency guarantees
  exercised against real row locks and transactions when the integration
  suite is run.

Run `go test ./... -cover` for the mock-backed tests. Run the PostgreSQL
integration suite separately with `make test-integration`.

## Tradeoffs and assumptions

- Amounts are `int64` in the smallest currency unit (e.g. cents); no
  currency field, since the assignment doesn't call for multi-currency
  support.
- `POST /wallets` isn't part of the assignment's required contract but is
  included as the only way to provision wallets for exercising the API
  (no admin/seed tooling otherwise). It's a thin convenience, not part of
  the transfer workflow.
- `READ COMMITTED` isolation with explicit `SELECT ... FOR UPDATE` locks
  was chosen over `SERIALIZABLE` isolation: it gives the same correctness
  guarantee for this access pattern (lock the rows you're about to
  mutate) without the added complexity of retrying serialization failures.
