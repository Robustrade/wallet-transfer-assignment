# Wallet Transfer Service — Design

A service for wallet-to-wallet transfers with idempotent requests, a double-entry
ledger, and safe concurrent execution. The core idea: enforce correctness in the
database (constraints + row locks), not in application code.

## Problem

Move funds between two wallets so that:

- A transfer is atomic — debit, credit, and both ledger entries commit together or not at all.
- The same request retried any number of times results in exactly one transfer (via a client `idempotencyKey`).
- A wallet can never be overdrawn, even under concurrent transfers out of it.
- The ledger uses double-entry accounting and always balances.

## API

### `POST /transfers`

Request:
```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

Response `201` (or `200` on an idempotent replay):
```json
{ "transferId": "T1", "status": "PROCESSED", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

| HTTP | Condition | code |
|------|-----------|------|
| 400 | missing field, `amount <= 0`, same wallet | `INVALID_REQUEST` |
| 404 | wallet not found | `WALLET_NOT_FOUND` |
| 409 | `idempotencyKey` reused with a different body | `IDEMPOTENCY_KEY_CONFLICT` |
| 422 | insufficient funds | `INSUFFICIENT_FUNDS` |

A repeat of the same key + same body returns the original stored response, never a second transfer.

Optional reads (if time permits): `GET /wallets/{id}`, `GET /wallets/{id}/transfers`, `GET /transfers/{id}`.

## Money representation

Amounts are integers in the smallest currency unit (`BIGINT`) — no floats, so
the non-negative balance check is exact.

## Schema

```sql
CREATE TABLE wallets (
    id         TEXT PRIMARY KEY,
    balance    BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id          TEXT PRIMARY KEY,
    from_wallet TEXT NOT NULL REFERENCES wallets(id),
    to_wallet   TEXT NOT NULL REFERENCES wallets(id),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    status      TEXT NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT no_self_transfer CHECK (from_wallet <> to_wallet)
);

CREATE TABLE ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    type        TEXT NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT one_entry_per_side UNIQUE (transfer_id, type)
);
CREATE INDEX idx_ledger_wallet ON ledger_entries (wallet_id);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash    TEXT NOT NULL,
    transfer_id     TEXT REFERENCES transfers(id),
    response_body   JSONB,
    status          TEXT NOT NULL CHECK (status IN ('IN_PROGRESS','COMPLETED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The constraints carry the correctness guarantees:

- `balance_non_negative` — the database physically rejects an overdraw.
- `idempotency_key` primary key — duplicate detection is a uniqueness guarantee, not a racy read-then-write.
- `ledger_entries (transfer_id, type)` unique — a transfer's ledger can't be double-posted.
- `no_self_transfer`, `amount > 0` — invalid transfers can't be persisted.

## Idempotency

The naive "check if the key exists, else do the work" is a check-then-act race —
two concurrent retries both pass the check. Instead the insert itself is the gate.

Inside one transaction:

1. `request_hash = sha256(canonical_json(body))`.
2. Insert the idempotency record as `IN_PROGRESS`.
   - Insert succeeds → we own this key; do the transfer.
   - Unique violation → load the existing record:
     - different `request_hash` → `409` (key reused for a different request).
     - same hash, `COMPLETED` → return the stored `response_body`.
     - same hash, `IN_PROGRESS` → a concurrent original is running; return "retry shortly", do not start a second transfer.
3. After the transfer, set the record to `COMPLETED` with the response, in the same transaction.

The record and the transfer commit together, so there's no window where one exists without the other.

## Concurrency

One transaction per request. Within it:

1. Lock both wallets with `SELECT ... FOR UPDATE`, ordered by wallet id. Deterministic
   lock order is what keeps `A→B` and `B→A` from deadlocking.
2. Re-read the source balance under the lock; if `balance < amount`, mark the transfer `FAILED` and return `422`.
3. Insert the transfer as `PENDING`.
4. Update both balances.
5. Insert the two ledger entries (DEBIT source, CREDIT destination).
6. `UPDATE transfers SET status='PROCESSED' WHERE id = :id AND status = 'PENDING'` — the guard makes the transition safe under duplicates.
7. Commit. Any exception rolls the whole thing back.

Isolation is `READ COMMITTED` (Postgres default); explicit `FOR UPDATE` locks over the
exact rows we mutate make a higher level unnecessary. Pessimistic locking is chosen
over optimistic because funds on a hot wallet should serialize rather than thrash on retries.

## State machine

```
PENDING → PROCESSED   (success, terminal)
PENDING → FAILED      (insufficient funds / error, terminal)
```

Only `PENDING` rows transition, guarded by `WHERE status = 'PENDING'`. Retries of a
completed transfer are short-circuited by the idempotency layer before any state work runs.

## Structure

```
app/
  api/          routers, request/response models, error mapping
  services/     transfer orchestration + idempotency workflow
  repositories/ wallet / transfer / ledger / idempotency persistence
  domain/       enums and validation rules
  db/           engine, session, migrations
tests/
```

- API: transport only — validation and HTTP mapping.
- Service: owns the transaction boundary and idempotency flow.
- Repository: persistence only — issues `FOR UPDATE`, inserts, guarded updates.
- Domain: `TransferStatus`, `EntryType`, amount rules.

Stack: FastAPI, Pydantic v2, SQLAlchemy 2.0, Alembic, PostgreSQL, pytest. Local run via docker-compose.

## Failure modes

| Failure | Handling |
|---------|----------|
| Duplicate request (same key + body) | replay returns the stored response, no new side effects |
| Same key, different body | `409` — never silently transfer |
| Concurrent debits, same wallet | `FOR UPDATE` serializes them; `CHECK` backstops |
| `A→B` and `B→A` at once | deterministic lock ordering prevents deadlock |
| Crash mid-transfer | single transaction rolls back, no partial state |
| Insufficient funds | transfer recorded `FAILED`, balances untouched |
| Retry after an unseen success | idempotency record is `COMPLETED`, original result returned |

## Testing

TDD order:

1. Happy path — funds move, two ledger rows, balances correct.
2. Idempotency — same key + body twice → one transfer, identical response.
3. Idempotency conflict — same key, different amount → `409`.
4. Insufficient funds — `422`, `FAILED`, no ledger rows.
5. Validation — `amount <= 0`, self-transfer, unknown wallet.
6. Concurrency — N threads debit one wallet funded for M < N; exactly M succeed, balance never negative.
7. Ledger invariant — `SUM(credits) - SUM(debits) = 0` after a batch of random transfers.

## Assumptions

- Wallets are seeded up front; wallet onboarding is out of scope.
- Single currency, integer minor units.
- Stored balance for O(1) reads, with the ledger as the auditable source of truth.
- `IN_PROGRESS` records left by a crash are cleared by transaction rollback plus a TTL sweep.
- No auth or rate limiting in scope.

## Plan

1. Scaffold FastAPI + SQLAlchemy + Alembic + docker-compose.
2. Migrations for the four tables.
3. Domain enums + request/response models.
4. Repositories.
5. Transfer service (idempotency + transactional transfer).
6. Router + error mapping.
7. Tests in the order above.
8. README with run instructions.
