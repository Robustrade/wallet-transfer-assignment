# Design: Wallet Transfer Service

## Problem statement

Support wallet-to-wallet transfers with exactly-once semantics at the API
level, a double-entry ledger, and correct balances under concurrent
transfers.

## Stack

- Go 1.24, `net/http` + `chi` router
- `database/sql` + `pgx` driver, hand-written SQL (no ORM) — the locking
  strategy needs to be visible, not hidden behind abstraction
- PostgreSQL, run locally via `docker-compose`
- `golang-migrate` for schema migrations
- `testify` for assertions; integration tests run against a real Postgres

## Schema

```sql
CREATE TABLE wallets (
  id          TEXT PRIMARY KEY,
  balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');

CREATE TABLE transfers (
  id              UUID PRIMARY KEY,
  from_wallet_id  TEXT NOT NULL REFERENCES wallets(id),
  to_wallet_id    TEXT NOT NULL REFERENCES wallets(id),
  amount          BIGINT NOT NULL CHECK (amount > 0),
  status          transfer_status NOT NULL DEFAULT 'PENDING',
  failure_reason  TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE ledger_entries (
  id           BIGSERIAL PRIMARY KEY,
  transfer_id  UUID NOT NULL REFERENCES transfers(id),
  wallet_id    TEXT NOT NULL REFERENCES wallets(id),
  entry_type   ledger_entry_type NOT NULL,
  amount       BIGINT NOT NULL CHECK (amount > 0),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (transfer_id, entry_type)
);
CREATE INDEX idx_ledger_wallet_id ON ledger_entries(wallet_id);

CREATE TYPE idempotency_status AS ENUM ('IN_PROGRESS', 'COMPLETED');

CREATE TABLE idempotency_records (
  idempotency_key      TEXT PRIMARY KEY,
  request_fingerprint  TEXT NOT NULL,
  status               idempotency_status NOT NULL DEFAULT 'IN_PROGRESS',
  transfer_id          UUID REFERENCES transfers(id),
  response_status      INT,
  response_body        JSON,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    (status = 'IN_PROGRESS' AND response_status IS NULL AND response_body IS NULL)
    OR
    (status = 'COMPLETED' AND response_status IS NOT NULL AND response_body IS NOT NULL)
  )
);
```

Amounts are `BIGINT` minor units (paise/cents) — avoids float rounding
errors, standard practice for money.

`transfers.id` and other UUIDs are generated in application code
(`google/uuid`), not by the database, so no `pgcrypto`/`gen_random_uuid()`
dependency is needed in the migration. `response_body` is `JSON`, not
`JSONB` — `JSON` preserves the exact input bytes, which is what makes a
replayed response byte-identical to the original rather than merely
semantically equivalent.

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

Responses:

- `201` — transfer created or idempotently replayed
  ```json
  {"id": "...", "status": "PROCESSED", "fromWalletId": "...", "toWalletId": "...", "amount": 100, "createdAt": "..."}
  ```
- `400` — invalid request body (missing fields, non-positive amount, from == to)
- `404` — `fromWalletId` or `toWalletId` does not exist
- `409` — insufficient funds, or the same `idempotencyKey` was reused with a
  different payload
- `503` — an in-flight duplicate request timed out waiting for the original
  to finish (see below); safe to retry

### `GET /healthz`

Liveness/readiness check — pings the DB.

## Idempotency strategy

One DB transaction handles the entire request. Postgres row locks are used
as the synchronization primitive for concurrent duplicates, instead of
application-level polling:

1. `INSERT INTO idempotency_records (...) VALUES (..., 'IN_PROGRESS')
   ON CONFLICT (idempotency_key) DO NOTHING`
2. `SELECT * FROM idempotency_records WHERE idempotency_key = $1 FOR UPDATE`
   with a `statement_timeout` set on the session.
   - If this request lost the insert race, this blocks on the row lock held
     by whoever is currently processing that key. When the other
     transaction commits, this unblocks and reads the `COMPLETED` row.
   - If the wait exceeds the timeout (e.g. the original request's process
     died mid-transaction), return `503` — the client can safely retry
     because the original transaction never committed and will have rolled
     back entirely.
3. If `status == COMPLETED`: compare `request_fingerprint` (hash of
   `fromWalletId`+`toWalletId`+`amount`). Mismatch → `409`. Match → replay
   the stored `response_status`/`response_body` verbatim.
4. If this request won the insert (row is `IN_PROGRESS`, no `transfer_id`
   yet): proceed to create the transfer in the same transaction, then
   update this row to `COMPLETED` with the response snapshot before commit.

**Crash safety**: idempotency record, transfer, ledger entries, and wallet
balance updates all commit atomically in one transaction. A crash before
commit rolls back everything — a retry with the same key reprocesses
cleanly from scratch. A crash after commit means the result is durably
stored and any later retry replays it. There is no partial-execution state
to reconcile.

## Concurrency strategy

- Wallets are locked with `SELECT ... FOR UPDATE` inside the transfer
  transaction — pessimistic locking, chosen over optimistic
  locking/versioning because it's simpler to reason about and makes the
  "no read-then-write race" property obvious on inspection.
- Both wallets are locked in a single query, **ordered by `id`**
  (`WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`), so two transfers moving
  money in opposite directions between the same pair of wallets always
  acquire locks in the same order and cannot deadlock.
- That same locking query also serves as the existence check — if it
  returns fewer than 2 rows, the missing wallet is a `404`, avoiding a
  separate lookup that could race with a concurrent delete/create.
- `balance >= 0` is enforced by a `CHECK` constraint as a DB-level backstop
  independent of application logic.
- Isolation level: `READ COMMITTED` (Postgres default) is sufficient — the
  explicit row locks provide the serialization needed; `SERIALIZABLE` would
  add retry-on-conflict handling for no benefit here.

## Layering

```
handler/    decode + validate JSON, map domain errors -> HTTP status. No SQL, no business rules.
service/    TransferService.CreateTransfer owns the transaction boundary described above.
repository/ WalletRepo, TransferRepo, LedgerRepo, IdempotencyRepo. Plain SQL against a passed
            *sql.Tx. No business logic, no transaction management of their own.
domain/     Wallet, Transfer, LedgerEntry, Transfer.CanTransition(to Status), amount validation.
```

Two different things flow out of the service, deliberately kept distinct:

- **Business outcomes** (insufficient funds, unknown wallet) are not Go
  errors — they're legitimate, completed results of a transfer attempt,
  so the service returns them as a `Result{StatusCode, Body}` like any
  successful transfer. This is also what lets them be cached under the
  idempotency key and replayed byte-for-byte, the same as a `PROCESSED`
  transfer.
- **Actual errors** (validation failures, idempotency key reuse with a
  different payload, the processing-wait timeout, unexpected DB errors)
  are typed in `domain` (`ErrSameWallet`, `ErrInvalidAmount`,
  `ErrIdempotencyConflict`, `ErrProcessingTimeout`, ...) and mapped to
  HTTP status codes in one place in the handler layer.

## Testing

- Domain: transfer state transition rules, amount validation.
- Service (against real test Postgres, not mocks): successful transfer;
  insufficient funds -> `FAILED` transfer with no ledger rows; idempotent
  replay returns an identical response with no new ledger rows or balance
  change; same key + different payload -> `409`; unknown wallet -> `404`.
- Concurrency integration test: N goroutines firing transfers out of one
  wallet with just enough combined balance for a subset to succeed; run
  with `go test -race`; assert final balance equals
  `initial - sum(successful transfers)`, never negative, and ledger entries
  net to zero per transfer.

## Observability

Structured logging (`log/slog`) with a request ID per call, logging
transfer outcome (processed/failed/replayed) and duration. `/healthz` for
liveness.

## Out of scope / deliberately not built

- Wallet creation and transfer history APIs (optional per the assignment;
  wallets are seeded via migration/test fixtures to keep the API surface
  scoped to what's required).
- Retry/backoff on the client side — the service guarantees safe retries,
  it doesn't implement a retrying client.
- Multi-currency — amounts are a single implicit currency/unit.
