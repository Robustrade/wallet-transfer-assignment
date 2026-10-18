# Wallet Transfer Service — Architecture

**Status:** Implemented design (aligned with the code)  
**Stack:** Go, Fiber v3, GORM, PostgreSQL, golang-migrate  
**Related:** `REQUIREMENTS.md`, `API.md`, `DATABASE.md`, `DECISIONS.md`, `TESTING.md`

## 1. Purpose and guarantees

Build a wallet-to-wallet transfer API that prevents duplicate execution for a given idempotency key, prevents overspending under concurrent requests, and commits wallet balances, a balanced two-entry ledger, and the final transfer state atomically.

The guarantee is **at most one committed business transfer per idempotency key**, with replay of the recorded result. Network delivery of an HTTP response is not exactly-once; a client may need to retry with the same key.

## 2. Components and boundaries

```text
HTTP Client
    |
    v
Fiber v3 handler (parse, validate shape, map errors/responses)
    |
    v
Transfer service (orchestrate, enforce business rules and idempotency)
    |                   \
    v                    v
Domain (entities/rules)   Repository interfaces
                              |
                              v
                     GORM + PostgreSQL
```

- **Handler:** Bind request, validate required fields, call service, return defined HTTP response. No SQL.
- **Service:** Compute canonical request fingerprint; execute the complete operation in a DB transaction; distinguish business rejection from transient infrastructure errors.
- **Domain:** Money validation, distinct-wallet rule, transfer states and legal transitions, ledger-entry construction. No Fiber/GORM dependencies.
- **Repository:** Persistence, unique-key claim, deterministic row locking, conditional balance updates, ledger inserts, transfer status update. All writes accept the same transaction handle.
- **Infrastructure:** PostgreSQL connection pool, SQL migrations, structured startup logging and a health check.

## 3. Deployment

```text
Go/Fiber v3 API process  ──►  PostgreSQL
```

The API reads its configuration from environment variables (`.env` in development) and connects to PostgreSQL through a pooled GORM connection. Schema migrations are applied explicitly with golang-migrate (`make migrate-up`), never via GORM AutoMigrate. The process shuts down gracefully on `SIGINT`/`SIGTERM`. `docker-compose.yml` runs PostgreSQL, a one-shot `migrate` job, a one-shot `seed` job and the API (see `docs/DATABASE.md` §8). It is a local/development mechanism; production would additionally require managed secrets, backups, monitoring, and appropriate database availability.

## 4. Monetary model

- `amount` and `balance` are `BIGINT` integers in the **smallest supported currency unit**; no floating point arithmetic.
- This design assumes one currency across all wallets. Currency support must be specified before multi-currency transfers.
- Amount must be positive; sender and recipient must differ; both wallets must exist.
- Reject values that would overflow `int64`; the database additionally enforces nonnegative balances and positive amounts.

## 5. PostgreSQL schema

The authoritative DDL is in `migrations/` (`000001`–`000004`). Abridged:

```sql
CREATE TABLE wallets (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id BIGINT NOT NULL REFERENCES wallets(id),
    to_wallet_id BIGINT NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id BIGINT NOT NULL REFERENCES transfers(id),
    wallet_id BIGINT NOT NULL REFERENCES wallets(id),
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transfer_id, entry_type)
);

CREATE TABLE idempotency_records (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    idempotency_key VARCHAR(64) NOT NULL UNIQUE
        CHECK (LENGTH(idempotency_key) BETWEEN 8 AND 64),
    request_hash VARCHAR(64) NOT NULL,
    transfer_id BIGINT REFERENCES transfers(id),
    response_status INT CHECK (response_status BETWEEN 100 AND 599),
    response_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((response_status IS NULL AND response_body IS NULL)
        OR (response_status IS NOT NULL AND response_body IS NOT NULL))
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_ledger_wallet_id ON ledger_entries(wallet_id);
```

**Important integrity boundary:** `UNIQUE (transfer_id, entry_type)` prevents duplicate debit/credit rows, but does **not by itself guarantee exactly two correct rows or that their amounts and wallet IDs match the transfer**. The service must insert the pair in one transaction and verify it. For stronger database-enforced invariants, add deferred constraint triggers or restrict direct table writes. All ledger rows are immutable after commit; restrict `UPDATE`/`DELETE` privileges.

The idempotency record is the concurrency claim: it is inserted first, and its response is filled in by the same transaction. `transfers.idempotency_key` is also unique, as a second guard. No partially completed idempotency record is committed.

## 6. Single-transaction transfer algorithm

The normal `POST /transfers` path is synchronous and uses **one PostgreSQL transaction**. `PENDING` is an intermediate state within the transaction; external readers normally see only the committed `PROCESSED` or `FAILED` outcome.

1. Validate request shape in the handler and the domain before opening the transaction; compute a canonical SHA-256 hash of the normalized `fromWalletId`, `toWalletId`, `amount` (and currency if added). The idempotency key itself is the lookup key, not a substitute for payload comparison.
2. Begin a GORM transaction (`READ COMMITTED` is sufficient with the explicit row locks and constraints described below).
3. Claim the key by inserting an `idempotency_records` row with `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING`. Concurrent identical-key inserts serialize on the unique index; one wins. **Do not do a read-then-insert check outside the transaction.**
4. If the claim did not win, read the committed record (a fresh statement under `READ COMMITTED`). If the stored hash differs, return `409 Conflict`. If it holds a response, return the stored status and body without touching balances or ledger. If it holds no response, return `409` "still in progress".
5. If the claim won, lock **both wallets in ascending wallet ID order**, explicitly acquiring each row lock in that order (`SELECT ... FOR UPDATE`). A missing wallet aborts the transaction with `404`.
6. Insert the `PENDING` transfer. This is done **after** the wallet locks: the foreign keys take `FOR KEY SHARE` locks on both wallets, so inserting first makes two concurrent transfers hold shared locks that each then try to upgrade to `FOR UPDATE`, which deadlocks.
7. Validate the balance while locked.
8. On a deterministic business rejection (insufficient funds), transition `PENDING -> FAILED`, save the failure response on the idempotency record, and **commit** without ledger rows or balance changes.
9. On success, check for receiver balance overflow, debit sender and credit recipient, insert exactly one `DEBIT` and one `CREDIT` ledger row for the same transfer and amount, transition `PENDING -> PROCESSED`, save the success response, and **commit**.
10. Return the saved HTTP response only after successful commit. On database errors, roll back and return an error; do not record such errors as terminal `FAILED` business outcomes.

**Atomicity:** balance changes, ledger entries, final status, and idempotency response either all commit or all roll back. A crash before commit leaves no committed transfer; a crash after commit allows a same-key retry to replay the result.

**Implementation detail:** The `ON CONFLICT DO NOTHING` result is checked for whether a row was actually inserted (`RowsAffected`). If another transaction holds the key, PostgreSQL waits for it to commit or roll back. Lock/statement timeouts are not configured yet and are listed as future work.

## 7. Locking, concurrency and deadlocks

- Acquire wallet row locks in **deterministic ascending ID order**, regardless of transfer direction. Lock each row sequentially rather than relying on an unspecified execution order in an unordered `IN (...)` query.
- Hold locks only during database work; no outbound HTTP calls or slow tasks inside the transaction.
- For two different transfers spending the same wallet, the second waits and checks the **updated** balance after the first commits; it cannot spend funds twice.
- Opposite-direction transfers use the same lock order, avoiding the classic A→B / B→A circular wait.
- Unique idempotency keys serialize duplicate attempts. Other deadlocks remain possible (e.g., interactions with additional tables or future operations): retry **the entire transaction** for SQLSTATE `40P01` (deadlock) or `40001` (serialization failure) with bounded attempts (3) and exponential backoff (100 ms, 200 ms).
- No blind retries after an **ambiguous commit result** (connection lost during commit): the service first queries by idempotency key in a new transaction and returns the saved response if it exists. Otherwise it reports `503` (outcome unknown, retry with the same key) rather than claiming failure.
- The connection pool is bounded (25 open / 10 idle connections). `lock_timeout`, `statement_timeout`, and request deadlines are future work.

## 8. Transfer state machine

```text
PENDING --> PROCESSED
    |
    +----> FAILED
```

- `PENDING`: created inside the transaction, not normally persisted as a visible standalone committed state.
- `PROCESSED`: successful debit, credit and ledger entries committed.
- `FAILED`: permanent business rejection committed, with a `failure_reason` and no ledger entries.
- No transition from either terminal state. Use guarded updates (`WHERE status = 'PENDING'`) and check affected-row count.
- Technical failures roll back the transaction, rather than committing `FAILED`.

If durable pending work and background recovery are introduced later, that is a **different processing model** requiring persisted `PENDING`, worker claims/leases, and recovery rules. It is not assumed here.

## 9. Idempotency contract

- The client supplies a unique key per logical transfer and **reuses it for retries**.
- A previously committed request with the same key and identical canonical payload returns its original recorded HTTP status and JSON response.
- The same key with different payload returns `409 Conflict`; no transfer occurs.
- Concurrent same-key requests cannot commit two transfers due to the unique constraint and transactional insertion.
- A committed business failure (e.g., insufficient funds) is replayed on same-key retries even if wallet funds change later. A new transfer requires a new key.
- This design keeps idempotency records indefinitely unless a separately specified retention policy is adopted. Deleting keys without a defined reuse window can break the deduplication guarantee.
- Key uniqueness is global to this service in the current schema. If accounts/tenants are introduced, scope the unique key and lookup to the authenticated owner.

## 10. API behavior (summary; exact contract in API.md)

`POST /transfers` request:

```json
{
  "idempotencyKey": "abc12345",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100
}
```

Outcomes:

| Case                                         | HTTP                       | Effect                                         |
| -------------------------------------------- | -------------------------- | ---------------------------------------------- |
| Successful new transfer                      | `201 Created`              | Transfer committed, two ledger entries         |
| Duplicate same key and payload               | Original stored status     | No additional side effects                     |
| Same key, different payload                  | `409 Conflict`             | No side effects                                |
| Same key, original still in progress         | `409 Conflict`             | No side effects                                |
| Insufficient balance                         | `409 Conflict`             | Committed `FAILED` transfer; no ledger entries |
| Wallet not found                             | `404 Not Found`            | Rolled back; key not reserved                  |
| Invalid input                                | `400 Bad Request`          | No transfer created                            |
| Unknown commit outcome                       | `503 Service Unavailable`  | Client retries with same key                   |
| Other / exhausted deadlock retries           | `500 Internal Server Error`| Rolled back; client may retry with same key    |

Errors use the envelope `{"error": "<message>"}`. Raw SQL errors and sensitive data are never exposed in API responses.

## 11. GORM and Fiber v3 implementation guidance

- Use a transaction manager (`WithinTransaction`, wrapping `Begin`/`Commit`/`Rollback`) so all repository calls use `tx`, not the root `db` handle.
- Use `gorm.io/gorm/clause` for row locking where practical (`clause.Locking{Strength: "UPDATE"}`), and raw SQL for explicit `INSERT ... ON CONFLICT ... RETURNING` when easier to reason about.
- Check `RowsAffected` for conditional updates and all returned errors. Ensure business rejection paths return `nil` from the GORM transaction callback **after persisting the `FAILED` outcome**, so the transaction commits.
- For unexpected SQL failures return an error from the callback so GORM rolls back.
- Keep Fiber handlers thin; do not pass Fiber-specific context objects deep into the domain. Propagate an appropriate `context.Context` to database operations.
- Use a dedicated error taxonomy: validation, idempotency conflict, insufficient funds, wallet not found, retryable DB error, unknown commit outcome.
- Never generate a new idempotency key during server-side retries.

## 12. Go project layout

```text
cmd/api/main.go
internal/
  bootstrap/            dependency wiring
  config/ database/     environment config, PostgreSQL connection
  middleware/           global HTTP middleware
  feature/wallet/
    domain/             entities, validation, state transitions
    model/              GORM models
    repository/         persistence, row locks, transaction manager
    service/            transfer workflow, idempotency, retries
    handler/            Fiber handler and error mapping
    route/              route registration
    integration/        PostgreSQL integration and concurrency tests
migrations/
docs/
Makefile
.env.example
README.md
```

Dependencies point one way: `route → handler → service → repository → domain/model`. Interfaces exist only where they have a real boundary (repositories and the transaction manager are faked in service tests).

## 13. Observability and operations

- Implemented: structured JSON startup/shutdown logs (`slog`), GORM warning/error logging (`DB_LOG_LEVEL`; `info` prints all SQL and is for debugging only), `GET /health`. Planned: request/correlation ID, transfer ID, outcome, duration and error category per request. Never log credentials or sensitive payloads unnecessarily.
- Metrics: transfer attempts and outcomes, idempotency replays/conflicts, DB transaction latency, deadlocks, lock timeouts, retry counts, ledger consistency audit failures.
- Health: a liveness endpoint (`GET /health`) exists; a readiness check against the database is planned.
- PostgreSQL migrations are versioned SQL files applied with golang-migrate.
- Database credentials via environment/secrets; `.env.example` contains placeholders only.
- Graceful shutdown is implemented; HTTP/database timeouts are future work.

## 14. Testing strategy

Unit tests (`make test`) run against in-memory fakes that enforce the schema's constraints. Transaction and locking behavior is proven against real PostgreSQL by `make test-integration`; unit tests alone cannot prove concurrent DB correctness.

| Scenario                       | Invariant                                                                                  |
| ------------------------------ | ------------------------------------------------------------------------------------------ |
| Successful transfer            | Sender decreases, recipient increases; one debit and one credit                            |
| Insufficient balance           | Committed `FAILED`, balances unchanged, zero ledger entries                                |
| Same key repeated sequentially | Original response; exactly one committed transfer                                          |
| Same key concurrent            | Exactly one committed transfer; other requests replay result                               |
| Same key, different payload    | `409`; no additional side effects                                                          |
| Two debits exceeding balance   | At most one succeeds; no negative balance                                                  |
| A→B and B→A concurrently       | No inconsistent balances; no unrecovered deadlock                                          |
| SQL failure mid-transfer       | Entire transaction rolled back                                                             |
| Connection loss before commit  | No partial state; same-key retry safe                                                      |
| Connection loss after commit   | Same-key retry returns original committed result                                           |
| Ledger reconciliation          | For every processed transfer: exactly two entries, equal positive amounts, correct wallets |

Commit-boundary behavior (failure before commit, uncertain commit) is tested with fault injection in the fake store; real locking is tested in the integration suite.

## 15. Decisions and open questions

**Chosen for this design:** synchronous single-transaction processing; PostgreSQL `READ COMMITTED` plus deterministic wallet row locks; stored balance with double-entry ledger; stable stored idempotent responses; business failures committed as `FAILED`.

**Decided:** nonexistent wallets return `404` and roll back (key not reserved); insufficient funds returns `409` with a committed `FAILED` transfer; error envelope is `{"error": "<message>"}`; exhausted deadlock retries return `500`.

**Still open:** monetary unit/currency; initial wallet provisioning/funding method; idempotency-key retention policy; authentication/tenant scope; request and lock timeouts. These choices should be captured in `API.md` / `DECISIONS.md` rather than silently assumed.
