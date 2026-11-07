# Wallet Transfer Service: Design Note

Written before implementation (docs-first). Updated whenever the code changes a decision.

## 1. Goal

`POST /transfers` moves money between two existing wallets:

- **atomic**: balances, ledger entries and transfer status change together or not at all;
- **exactly-once per `idempotencyKey`**: retries return the original result and never move money twice;
- **double-entry**: every processed transfer writes one DEBIT and one CREDIT of the same amount;
- **safe under concurrency**: a wallet can never be overdrawn.

Out of scope: auth, multi-currency, fees, reversals, wallet creation API.

## 2. Decisions

| Concern | Decision |
|---|---|
| Stack | Go 1.24, PostgreSQL 16, `pgx/v5` |
| Money | `BIGINT` minor units, never floats |
| Balance | Stored on `wallets`, updated in the same transaction as the ledger rows |
| Concurrency | `READ COMMITTED` + lock both wallets with `SELECT … ORDER BY id FOR NO KEY UPDATE` as the **first** statement |
| Idempotency | `UNIQUE` key on `transfers`, claimed with `INSERT … ON CONFLICT DO NOTHING` |
| Transaction | One DB transaction per transfer, owned by the service layer |

## 3. API

```json
POST /transfers
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

Validation (nothing persisted on failure): `amount` is an integer `> 0`; both wallet ids are non-empty and
different; `idempotencyKey` is optional and, if present, 1–255 chars; unknown JSON fields are rejected.

| Outcome | HTTP | Body |
|---|---|---|
| Processed | `201` | transfer, `status: PROCESSED` |
| Insufficient funds (`balance < amount`) | `422` | transfer, `status: FAILED`, `failureReason: INSUFFICIENT_FUNDS` |
| Same key, same payload | same status and body as the original, plus `Idempotent-Replayed: true` | original transfer |
| Same key, different payload | `409` | `IDEMPOTENCY_KEY_REUSED` |
| Invalid request | `400` | `VALIDATION_ERROR` |
| Wallet does not exist | `404` | `WALLET_NOT_FOUND` (nothing persisted, no wallet created) |
| Lock timeout / DB unavailable | `503` | `RETRYABLE`, rolled back, safe to retry with the same key |

## 4. Schema

```sql
CREATE TABLE wallets (
    id         TEXT        PRIMARY KEY,
    balance    BIGINT      NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id              UUID        PRIMARY KEY,
    idempotency_key TEXT        UNIQUE CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    status          TEXT        NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);
CREATE INDEX ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX ON transfers (to_wallet_id, created_at DESC);

CREATE TABLE ledger_entries (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id UUID        NOT NULL REFERENCES transfers(id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets(id),
    entry_type  TEXT        NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type)
);
CREATE INDEX ON ledger_entries (wallet_id, id);
```

The database enforces these even if the application has a bug: one transfer per key, no overdraft
(`balance >= 0` for every wallet), no ledger row without a transfer, at most one DEBIT and one CREDIT per transfer.
"Exactly two entries per PROCESSED transfer" is guaranteed by writing them in the same transaction and is
asserted in tests.

**No separate `idempotency_records` table.** The transfer row *is* the result to replay, so a second table would
duplicate state that could drift. A separate table becomes worthwhile if several endpoints need idempotency or
keys need to expire.

Wallets are seeded with an opening balance (migrations or tests). Opening balances have no ledger entry, so the
invariants we check are per transfer, plus "total balance across all wallets is unchanged".

## 5. Workflow (one transaction)

```
BEGIN
1. SELECT id, balance FROM wallets WHERE id IN ($from, $to) ORDER BY id FOR NO KEY UPDATE
     fewer than 2 rows → ROLLBACK, 404
2. INSERT INTO transfers (…, status 'PENDING') ON CONFLICT (idempotency_key) DO NOTHING RETURNING id
     no row → key already used: ROLLBACK, load that transfer, compare payload → replay or 409
3. from.balance < amount → UPDATE transfers SET status 'FAILED' WHERE id = $id AND status = 'PENDING'; COMMIT → 422
4. UPDATE wallets: debit from, credit to
5. INSERT ledger_entries: DEBIT(from), CREDIT(to)
6. UPDATE transfers SET status 'PROCESSED' WHERE id = $id AND status = 'PENDING'; COMMIT → 201
```

- **Payload comparison:** every request field is stored as a column, so `Transfer.MatchesRequest(req)` compares
  `from`, `to` and `amount`. The row is already loaded by the key lookup, so this costs nothing extra and needs no
  hash column. A test changes each request field and asserts a mismatch, so a newly added field can't be forgotten.
  Add a `request_hash` only if requests gain fields we don't store.
- **Status changes:** the domain allows only `PENDING → PROCESSED` and `PENDING → FAILED`. The SQL repeats the
  `WHERE status = 'PENDING'` guard, so a terminal transfer can never change.
- **`PENDING` never escapes the transaction.** There is no external side effect, so committing PENDING first would
  only create a window where a crash leaves half-done transfers. If an external payment rail is added, split this
  into "commit PENDING, call out, then finish" with a recovery job.
- **Insufficient funds is persisted as FAILED** so a retry with the same key returns the same failure, even if the
  wallet is topped up in between.

## 6. Concurrency

- **Double spend:** step 1 locks both wallets. A concurrent transfer touching either wallet waits there, then reads
  the committed balance, so the funds check cannot use a stale value. `CHECK (balance >= 0)` is the backstop.
- **Same key, concurrent requests:** the second `INSERT … ON CONFLICT` waits on the first transaction's unique-index
  entry. If the first commits, the second replays its result. If the first rolls back, the second does the
  transfer. The unique index, not application code, picks the winner.
- **No deadlocks:** every wallet lock is taken in step 1, in ascending id order, so there are no lock cycles.
  - Opposing transfers (A→B, B→A) both queue on the lower id first.
  - Locking *after* inserting the transfer would deadlock: the FK check takes `FOR KEY SHARE` on the wallets, and
    two transactions sharing a wallet would each wait to strengthen that lock. Locking first, with
    `FOR NO KEY UPDATE` (which is compatible with `FOR KEY SHARE`), avoids this.
- **Timeouts:** `lock_timeout` and `statement_timeout` per transaction, mapped to `503`.

Why not the alternatives: `SERIALIZABLE` and optimistic locking both need retry loops and degrade under a busy
wallet. A single conditional `UPDATE … WHERE balance >= $amount` works, but it can't tell "wallet missing" from
"insufficient funds" and hides the business rule in SQL.

## 7. Failures and retries

| Scenario | Result |
|---|---|
| Crash before commit | Rolled back, key not used, retry executes normally |
| Commit succeeded, response lost | Retry replays the original result |
| Process restart | All state is in Postgres, same as above |
| Request without a key | Not deduplicated (per assignment), client's responsibility |

## 8. Code layout

```
cmd/server/                    wiring, config, graceful shutdown
internal/domain/               Transfer, Wallet, status rules, validation, errors (no I/O)
internal/service/              transfer workflow + idempotency; owns the transaction
internal/repository/           interfaces the service depends on (Repository, TxManager)
internal/repository/postgres/  SQL only, maps Postgres errors to domain errors
internal/handler/              HTTP decode/validate → service → status codes
migrations/                    SQL, embedded and applied at startup
```

Logging: one structured `slog` line per transfer request (`transfer_id`, `idempotency_key`, outcome, duration).

## 9. Tests

- **Domain unit:** validation, funds check, status rules, `MatchesRequest`.
- **Service against real Postgres** (testcontainers): success, insufficient funds, unknown wallet, replay,
  key reuse with a different payload, and concurrency: N parallel debits that only k can afford, N parallel
  requests with the same key, opposing and overlapping transfers with no deadlocks. Every test checks the ledger
  invariants afterwards.
- **Handler:** status-code and error mapping with a fake service.
