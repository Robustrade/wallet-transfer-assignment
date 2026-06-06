# Database Schema

## Problem Statement

The service requires a persistent store that enforces correctness guarantees at
the database level — not just in application code. Schema constraints, indexes,
and foreign keys act as the last line of defence against data corruption.

## Schema Overview

### `wallets`

Stores wallet identity and running balance.

```sql
id         UUID        PRIMARY KEY
balance    BIGINT      NOT NULL DEFAULT 0  CHECK (balance >= 0)
version    BIGINT      NOT NULL DEFAULT 0
created_at TIMESTAMP   NOT NULL DEFAULT NOW()
updated_at TIMESTAMP   NOT NULL DEFAULT NOW()
```

- Primary key is indexed, hence, ID based lookups are the fastest lookups
- `balance BIGINT` for amount in cents
- `balance >= 0` constraint prevents overdraft at the DB level even if application
  logic has a bug.
- `version` is incremented on every balance update; used as an optimistic-lock
  guard for future read-heavy paths.

### `transfers`

Records a transfer request and its lifecycle.

```sql
id              UUID        PRIMARY KEY
idempotency_key TEXT        NOT NULL  UNIQUE
from_wallet_id  UUID        NOT NULL  REFERENCES wallets(id)
to_wallet_id    UUID        NOT NULL  REFERENCES wallets(id)
amount          BIGINT      NOT NULL CHECK (amount > 0)
status          SMALLINT    NOT NULL DEFAULT 1  CHECK (status IN (1,2,3))
created_at      TIMESTAMP   NOT NULL DEFAULT NOW()
updated_at      TIMESTAMP   NOT NULL DEFAULT NOW()
```

Constraints:
- `UNIQUE (idempotency_key)` — database-level guard against duplicate transfers.
- `CHECK (from_wallet_id <> to_wallet_id)` — prevents self-transfers.
- `CHECK (amount > 0)` — non-positive amounts are rejected.
- `status IN (1,2,3)` — only valid domain states persist (1=PENDING, 2=PROCESSED,
  3=FAILED).

### `ledger_entries`

Double-entry bookkeeping; every transfer produces exactly two rows.

```sql
id          UUID        PRIMARY KEY
transfer_id UUID        NOT NULL  REFERENCES transfers(id)
wallet_id   UUID        NOT NULL  REFERENCES wallets(id)
type        SMALLINT    NOT NULL  CHECK (type IN (1,2))
amount      BIGINT      NOT NULL CHECK (amount > 0)
created_at  TIMESTAMP   NOT NULL DEFAULT NOW()
```

- `type IN (1,2)` — only DEBIT (1) or CREDIT (2) are valid.
- `REFERENCES transfers(id)` — entries cannot exist without a parent transfer.

Indexes:
```sql
idx_ledger_transfer ON ledger_entries(transfer_id)
idx_ledger_wallet   ON ledger_entries(wallet_id)
```

### `idempotency_records`

Caches full HTTP response for each idempotency key.

```sql
key           TEXT     PRIMARY KEY
transfer_id   UUID     NOT NULL
response_json TEXT     NOT NULL
status_code   SMALLINT NOT NULL
created_at    TIMESTAMP   NOT NULL DEFAULT NOW()
```

- Written atomically within the same transaction as the transfer.
- On duplicate request, the service reads this table first and returns the cached
  response — no business logic is re-executed.

## Index Strategy

System relies on using primary keys for table look-ups; the fastest look-up 
strategy by-far.

Other indexes for foreign-keys:

| Index | Reason |
|---|---|
| `idx_transfers_from_wallet` | Filter/join for transfer history per source wallet |
| `idx_transfers_to_wallet` | Filter/join for transfer history per destination wallet |
| `idx_transfers_status` | Filter by status in monitoring/admin queries |
| `idx_ledger_transfer` | Fast ledger lookup per transfer |
| `idx_ledger_wallet` | Fast balance computation or audit per wallet |

## Migration Strategy

Migrations live in `migrations/` and follow golang-migrate's numbered prefix
convention (`000001_init_schema.up.sql` / `000001_init_schema.down.sql`).

The `db.Migrate()` function is called at server startup before the HTTP listener
opens, so the schema is always up to date before traffic is accepted.

## Consistency Expectations

All writes to `wallets`, `transfers`, and `ledger_entries` happen inside a single
PostgreSQL transaction. Either all three succeed together or none of them persist.

## Failure Modes

| Failure | Behaviour |
|---|---|
| `balance >= 0` check violation | Transaction rolls back; `ErrInsufficientFunds` returned |
| Duplicate `idempotency_key` insert | Unique constraint raises `23505`; service detects and returns cached response |
| Migration fails at startup | Server exits with non-zero code; no HTTP port opens |
