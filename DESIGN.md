# Design Note — Wallet Transfer Service

## Problem Statement

Provide an HTTP service for wallet-to-wallet transfers with idempotent request
handling, a double-entry ledger, correct balances under concurrency, and a
transfer state machine (`PENDING -> PROCESSED|FAILED`).

## API Contract

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

Response `201 Created` (first time) or `200 OK` (idempotent replay):

```json
{
  "transferId": "uuid",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "createdAt": "2026-09-06T10:00:00Z"
}
```

Error responses (`4xx`) use:

```json
{ "error": "insufficient_funds", "message": "wallet wallet_1 has insufficient balance" }
```

Failure modes and status codes:

| Condition                              | Status | Error code            |
|-----------------------------------------|--------|------------------------|
| missing/invalid fields, amount <= 0     | 400    | `invalid_request`      |
| `fromWalletId == toWalletId`            | 400    | `invalid_request`      |
| wallet not found                        | 404    | `wallet_not_found`     |
| insufficient balance                    | 409    | `insufficient_funds`   |
| idempotency key reused with different payload | 409 | `idempotency_key_conflict` |
| same key + same payload replayed        | 200    | (original result)      |

### `GET /wallets/{id}` (optional enhancement)

Returns current wallet balance.

### `GET /transfers/{id}` (optional enhancement)

Returns transfer status and its two ledger entries.

## Data Model

```
wallets
  id            TEXT PRIMARY KEY
  balance       INTEGER NOT NULL CHECK (balance >= 0)   -- minor units (cents)
  created_at    TIMESTAMP NOT NULL
  updated_at    TIMESTAMP NOT NULL

transfers
  id                 TEXT PRIMARY KEY
  idempotency_key    TEXT NOT NULL UNIQUE
  from_wallet_id     TEXT NOT NULL REFERENCES wallets(id)
  to_wallet_id       TEXT NOT NULL REFERENCES wallets(id)
  amount             INTEGER NOT NULL CHECK (amount > 0)
  status             TEXT NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED'))
  failure_reason     TEXT
  request_hash       TEXT NOT NULL   -- sha256 of normalized request body, for key-reuse detection
  created_at         TIMESTAMP NOT NULL
  updated_at         TIMESTAMP NOT NULL
  CHECK (from_wallet_id <> to_wallet_id)

ledger_entries
  id            TEXT PRIMARY KEY
  transfer_id   TEXT NOT NULL REFERENCES transfers(id)
  wallet_id     TEXT NOT NULL REFERENCES wallets(id)
  type          TEXT NOT NULL CHECK (type IN ('DEBIT','CREDIT'))
  amount        INTEGER NOT NULL CHECK (amount > 0)
  created_at    TIMESTAMP NOT NULL
```

Indexes: unique on `transfers.idempotency_key`; index on `ledger_entries.wallet_id`
and `ledger_entries.transfer_id` for lookups.

No separate `idempotency_records` table — `transfers.idempotency_key` (unique)
plus `request_hash` covers detection, storage, and replay without duplicating
the transfer row elsewhere. The stored `transfers` row *is* the durable
idempotency record, and the response is fully reconstructible from it plus its
ledger entries, so no separate response blob needs to be persisted.

## Idempotency Strategy

1. Compute `requestHash = sha256(fromWalletId|toWalletId|amount)`.
2. Attempt to `INSERT` a `transfers` row with status `PENDING` inside the same
   transaction that will perform the transfer. The `UNIQUE` constraint on
   `idempotency_key` is the source of truth — not a prior `SELECT`.
3. If the insert violates the unique constraint:
   - Load the existing transfer by key.
   - If `request_hash` differs, return `409 idempotency_key_conflict`.
   - Otherwise, return the existing transfer's current state as the response
     (whatever it is — `PENDING`/`PROCESSED`/`FAILED`) without re-executing
     any balance mutation.
4. If the insert succeeds, this request "owns" the transfer and proceeds to
   execute it in the same transaction (see below), then commits.

This makes duplicate detection atomic with respect to concurrent identical
requests: two concurrent requests with the same key race on the same unique
insert, only one wins, the other reads back the winner's committed result.
It is also safe across process restarts, since the record lives in the
database, not in memory.

## Concurrency Strategy

All balance mutation for a transfer happens in one DB transaction:

1. Insert the `PENDING` transfer row (idempotency guard above).
2. Lock both wallet rows for update, **always in a fixed order (ascending by
   wallet id)** regardless of debit/credit direction, to prevent lock-order
   deadlocks between two opposite-direction concurrent transfers.
3. Re-check the source wallet's balance under the lock. If insufficient,
   update the transfer to `FAILED` with a reason and commit (a failed
   transfer is still a terminal, recorded outcome — not an error that rolls
   back the whole row).
4. Debit the source, credit the destination, insert both `ledger_entries`,
   set the transfer to `PROCESSED`.
5. Commit.

Chosen mechanism: **row-level locking via explicit lock acquisition in a
single transaction**, not optimistic locking. Rationale: transfers are
short-lived and contention is expected to be on "hot" wallets, so pessimistic
locking avoids retry storms that optimistic concurrency would cause under
contention, and it keeps the logic simpler to reason about (no
CAS-and-retry loop).

Database note: the reference implementation uses SQLite (`modernc.org/sqlite`,
pure Go, no cgo required) as the acceptable alternative persistence store.
SQLite does not support row-level `SELECT ... FOR UPDATE`; instead, writer
transactions are opened with `BEGIN IMMEDIATE`, which acquires a write lock on
the whole database up front and serializes all writers. This gives the same
correctness guarantee (no lost updates, no double spend) as row-level locking
would on Postgres, at the cost of coarser-grained concurrency (writers queue
globally instead of only on the specific wallets involved). On Postgres, the
same code path would instead use `SELECT ... FOR UPDATE ORDER BY id` on the
two wallet rows inside a `READ COMMITTED` transaction, which is noted in code
comments at the lock-acquisition boundary so the swap is localized to the
repository layer.

## Retry / Failure Behavior

- Network failure after commit but before the client sees the response: safe
  — client retries with the same idempotency key, gets the committed result.
- Crash mid-transaction: the DB transaction never commits, so no partial
  state (no orphan ledger rows, no PENDING-forever transfer) is visible.
- Insufficient funds is a **business failure**, not a system error: it is
  still recorded as a terminal `FAILED` transfer with two-sided consistency
  preserved (no ledger entries are written for a failed transfer).

## Observability

- Structured log line per transfer attempt: key, wallet ids, amount, outcome,
  duration.
- `GET /wallets/{id}` and `GET /transfers/{id}` (optional) double as manual
  verification/debugging endpoints.

## Testing Strategy

- Domain: state transition rules (valid/invalid transitions rejected).
- Service, happy path: transfer moves funds, produces exactly 2 balanced
  ledger entries, status `PROCESSED`.
- Service, idempotency: same key replayed serially returns identical result,
  no duplicate ledger entries; same key with a different payload is rejected.
- Service, insufficient funds: transfer marked `FAILED`, balances unchanged,
  no ledger entries written.
- Concurrency: N goroutines issuing concurrent transfers out of the same
  wallet; assert final balance is exactly `initial - sum(successful transfers)`
  and never negative, and ledger entries sum to zero net across all wallets.

## Layering

```
handler/http   -> request validation, JSON (de)serialization, status mapping
service        -> orchestration, idempotency + transaction boundary, state machine invocation
repository     -> SQL only, no business rules
domain         -> Wallet/Transfer/LedgerEntry entities, state transition rules, validation
```
