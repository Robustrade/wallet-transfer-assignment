# Implementation Prompt — Wallet Transfer Service

**Audience:** Claude Sonnet 5 (implementer)
**Author:** Claude Opus 5 (architecture/planning pass), reviewed by the repo owner
**Source of truth for requirements:** [`ASSIGNMENT.md`](./ASSIGNMENT.md), [`evaluation_guide.md`](./evaluation_guide.md), [`.github/copilot-instructions.md`](./.github/copilot-instructions.md), [`.github/pull_request_template.md`](./.github/pull_request_template.md)

---

## 0. How to use this document

This is a **complete build specification**, not a sketch. Every architectural decision below has already been made and justified. Your job is to implement it faithfully, test it hard, and commit it as a readable history.

Rules of engagement:

1. **Do not re-litigate the design.** If you believe something here is *wrong* (not merely different from your preference), implement the correct thing and record the deviation in `docs/design.md` under a `## Deviations from the original plan` heading. Do not silently change the contract.
2. **Do not ask clarifying questions before starting.** Every open question has been resolved in this document. If you hit a genuine blocker (e.g., Postgres unreachable), state it plainly and continue with everything not blocked by it.
3. **Do not gold-plate.** The submission must read like 3–5 hours of focused senior work. Section 14 lists explicit anti-goals. Adding things from that list will *lower* the score — `copilot-instructions.md` says reviewers will "suggest simpler designs when the solution is overengineered".
4. **Do not push or open a pull request.** Commit locally on a branch. Write the PR body to `docs/pr-description.md`. The repo owner pushes and opens the PR.
5. **Report honestly.** If a test fails, a lint step can't run locally, or you skipped something, say so explicitly in your final message. Never claim a verification you didn't run.

---

## 1. What you are building

A single-service HTTP API for **wallet-to-wallet transfers** with:

- exactly-once semantics at the API level, keyed on `idempotencyKey`
- double-entry ledger recording (exactly two entries per transfer)
- correct balances under concurrent debits of the same wallet (no double spend, no negative balances)
- a guarded `PENDING → PROCESSED | FAILED` state machine
- clean handler / service / repository / domain layering

The graded artifacts are: the schema, the transaction strategy, the idempotency strategy, the layering, the tests, **and the commit history + docs**.

---

## 2. Stack and toolchain (fixed — do not substitute)

| Concern | Choice | Why |
|---|---|---|
| Language | **Go**, `go 1.24.0` in `go.mod` | `.github/workflows/ci.yml` pins `go-version: '1.24'`. Local toolchain is 1.26.5 and will build a 1.24 module fine. **Do not use stdlib APIs newer than Go 1.24.** |
| HTTP | **stdlib `net/http`** with Go 1.22+ `ServeMux` patterns (`mux.HandleFunc("POST /transfers", ...)`, `r.PathValue("id")`) | No router dependency needed; reviewers reward a small dependency surface. |
| DB driver | **`github.com/jackc/pgx/v5`** (`pgxpool`) | Native Postgres, first-class `pgx.Tx`, typed error codes via `*pgconn.PgError`. |
| DB | **PostgreSQL 16** (`postgres:16-alpine` via compose) | `ASSIGNMENT.md` states Postgres is preferred. Matches the owner's existing stack. |
| Migrations | Plain SQL under `internal/postgres/migrations/`, embedded with `embed.FS`, applied by a ~70-line in-repo migrator | Avoids a migration-framework dependency; the SQL stays reviewable as the schema deliverable. |
| IDs | `github.com/google/uuid`, **`uuid.NewV7()`** for transfer IDs | Time-ordered UUIDs give B-tree locality on the PK; a one-line justification in the design doc. |
| Tests | stdlib `testing` + `github.com/stretchr/testify/require` | Readable assertions without a framework. |
| Metrics | `github.com/prometheus/client_golang` (**stretch, last commit, droppable**) | Optional enhancement in `ASSIGNMENT.md`. |
| Logging | stdlib **`log/slog`**, JSON handler | No dependency, structured, idiomatic for Go 1.24. |
| Lint | `golangci-lint` **v1.64.8** (v1 config schema — *not* the v2 `version: 2` format) | CI pins this exact version; a v2-format `.golangci.yml` will fail to parse. |

**Total new direct dependencies: 4** (`pgx`, `uuid`, `testify`, `prometheus` if kept). Keep it that way.

### 2.1 Integration-test database strategy (important)

**Do not use testcontainers.** The owner's active Docker context is a remote SSH host (`oms-server`); testcontainers would try to reach it and hang or fail. Instead:

- Integration tests read **`TEST_DATABASE_URL`**. If it is unset, they `t.Skip` with the exact message:
  `set TEST_DATABASE_URL to run integration tests (try: make test)`
- `make db-up` starts Postgres via `docker compose`, with `DOCKER_CONTEXT ?= desktop-linux` pinned at the top of the Makefile (mirroring the owner's `ridego` Makefile) and overridable: `make db-up DOCKER_CONTEXT=default`.
- `make test` = `db-up` → wait for healthy → `TEST_DATABASE_URL=... go test ./... -race -count=1`.
- `make test-unit` = `go test ./... -race -count=1 -short` — must pass on a machine with no Docker and no Postgres.

This means `go test ./...` is always green, and the integration suite is one `make` away.

---

## 3. Repository layout

Create exactly this. Do not add packages not listed without a reason recorded in `docs/design.md`.

```
.
├── cmd/
│   └── walletd/
│       └── main.go                       # wiring, config load, migrate, serve, graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go                     # env parsing + defaults + validation
│   ├── domain/                           # NO imports of pgx, net/http, or any infra
│   │   ├── errors.go                     # sentinel errors + domain error type
│   │   ├── money.go                      # minor-unit amount + currency rules
│   │   ├── wallet.go                     # Wallet entity, Kind, Debit/Credit guards
│   │   ├── transfer.go                   # Transfer entity, Status, transition rules, fingerprint
│   │   ├── transfer_test.go
│   │   ├── money_test.go
│   │   └── ledger.go                     # Entry, EntryType, DoubleEntry construction
│   ├── service/
│   │   ├── ports.go                      # consumer-defined repo interfaces + UnitOfWork
│   │   ├── transfer_service.go           # THE core: idempotent claim + settlement
│   │   ├── wallet_service.go             # create wallet, get balance, list entries
│   │   ├── transfer_service_test.go      # unit tests against fakes
│   │   ├── wallet_service_test.go
│   │   └── fakes_test.go                 # in-memory UnitOfWork + repos
│   ├── postgres/
│   │   ├── db.go                         # pool ctor, health check, tx runner + retry
│   │   ├── migrate.go                    # embedded migrator + advisory lock
│   │   ├── errors.go                     # pgerrcode → domain error mapping
│   │   ├── wallet_repository.go
│   │   ├── transfer_repository.go
│   │   ├── ledger_repository.go
│   │   ├── migrations/
│   │   │   ├── 0001_init.up.sql
│   │   │   ├── 0001_init.down.sql
│   │   │   ├── 0002_seed_system_wallets.up.sql
│   │   │   └── 0002_seed_system_wallets.down.sql
│   │   └── schema_integration_test.go    # DB-level invariants (triggers, constraints)
│   ├── httpapi/
│   │   ├── router.go                      # ServeMux + middleware chain
│   │   ├── dto.go                         # request/response structs + mapping
│   │   ├── transfers_handler.go
│   │   ├── wallets_handler.go
│   │   ├── system_handler.go              # /healthz, /readyz
│   │   ├── errors.go                      # domain error → HTTP status/code mapping
│   │   ├── middleware.go                  # request id, slog access log, panic recovery
│   │   ├── transfers_handler_test.go      # httptest against a fake service
│   │   └── wallets_handler_test.go
│   └── observability/
│       ├── logging.go
│       └── metrics.go                     # stretch; delete if you drop metrics
├── test/
│   └── integration/
│       ├── main_test.go                   # TestMain: connect, migrate, truncate helpers
│       ├── helpers_test.go                # HTTP test server, seed/fund helpers, invariant checks
│       ├── transfer_test.go               # happy path, failure, replay, ledger shape
│       ├── idempotency_test.go            # sequential + concurrent duplicate keys
│       └── concurrency_test.go            # double-spend, bidirectional deadlock, invariants
├── docs/
│   ├── design.md                          # THE documentation-first artifact (write FIRST)
│   ├── api.md                             # endpoint contract + examples
│   ├── ai-usage.md                        # AI disclosure required by ASSIGNMENT.md §AI usage
│   └── pr-description.md                  # filled-in .github/pull_request_template.md
├── .github/workflows/solution-ci.yml      # additive; DO NOT edit the existing workflows
├── .golangci.yml
├── .env.example
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod / go.sum
├── prompt.md                              # this file — keep it, it is part of the AI disclosure
└── README.md                              # rewrite: solution README (keep a short "about the template" note)
```

Module path: **`github.com/devthedeveloper/wallet-transfer-assignment`** (matches `git remote -v`).

---

## 4. Data model

### 4.1 Design decisions to implement (and to justify in `docs/design.md`)

1. **Money is `BIGINT` minor units** (paise/cents) — never floats. The API field is `amount` (integer, minor units), matching the assignment's example payload verbatim.
2. **Balances are stored on `wallets` *and* derivable from `ledger_entries`.** The stored balance is the authority for the debit check (so it can be row-locked); the ledger is the immutable audit truth. A reconciliation invariant test asserts they agree. Explain this both-and choice — it is the answer to `ASSIGNMENT.md` FR#2.
3. **Idempotency is enforced by `UNIQUE (idempotency_key)` on `transfers`** — the transfer row *is* the idempotency record.
   You must include this paragraph (in your own words) in `docs/design.md`:
   > A separate `idempotency_records` table was considered and deliberately not added. The service has exactly one mutating endpoint and its response is fully derivable from the `transfers` row, so a second table would duplicate state and introduce a consistency question (what if the record and the transfer disagree?) without adding a guarantee. The unique index is the dedup mechanism; a dedicated table becomes worthwhile when multiple endpoints need replay, when responses are not derivable from a domain row, or when keys need TTL/expiry.
4. **System wallets.** `wallets.kind ∈ {USER, SYSTEM}`. Only `SYSTEM` wallets may go negative; they model the boundary between this service and the outside world (deposits/withdrawals). Consequence: **no money is ever created outside the ledger**, and `SUM(balance_minor) = 0` across all wallets is a testable global invariant. `POST /wallets` creates USER wallets at zero balance; funding happens by transferring from the seeded `treasury_inr` system wallet.
5. **`balance_after` is recorded on every ledger entry.** Cheap, and it makes audit/debugging and test assertions precise.
6. **The database enforces what the database can enforce**: non-negative user balances, positive amounts, distinct wallets, exactly one DEBIT and one CREDIT per transfer, append-only ledger, and legal status transitions. Application logic is the fast path; constraints are the backstop. `evaluation_guide.md` asks "does the code rely on application logic where the database should enforce correctness?" — the answer must be no.

### 4.2 `0001_init.up.sql` — write this, essentially verbatim

```sql
-- Wallet transfer service — initial schema.
--
-- Design notes:
--   * Money is stored as BIGINT minor units (e.g. paise). Never floating point.
--   * ledger_entries is the append-only audit truth; wallets.balance_minor is the
--     lockable, fast-to-read projection of it. An invariant test asserts they agree.
--   * Only SYSTEM wallets may hold a negative balance; they represent the boundary
--     between this service and the outside world, which keeps SUM(balance_minor) = 0.

CREATE TYPE transfer_status   AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');
CREATE TYPE wallet_kind       AS ENUM ('USER', 'SYSTEM');

CREATE TABLE wallets (
    id            TEXT        PRIMARY KEY,
    kind          wallet_kind NOT NULL DEFAULT 'USER',
    currency      CHAR(3)     NOT NULL,
    balance_minor BIGINT      NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallets_id_format       CHECK (id ~ '^[A-Za-z0-9_-]{1,64}$'),
    CONSTRAINT wallets_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
    -- The double-spend backstop: application logic may only ever be the fast path.
    CONSTRAINT wallets_non_negative    CHECK (kind = 'SYSTEM' OR balance_minor >= 0)
);

CREATE TABLE transfers (
    id              UUID            PRIMARY KEY,
    idempotency_key TEXT            NOT NULL,
    request_hash    CHAR(64)        NOT NULL,   -- sha256 of the canonical request
    from_wallet_id  TEXT            NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    to_wallet_id    TEXT            NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    amount_minor    BIGINT          NOT NULL,
    currency        CHAR(3)         NOT NULL,
    status          transfer_status NOT NULL DEFAULT 'PENDING',
    failure_code    TEXT,
    failure_message TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    settled_at      TIMESTAMPTZ,
    -- Exactly-once at the API level lives on this one line.
    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key),
    CONSTRAINT transfers_amount_positive  CHECK (amount_minor > 0 AND amount_minor <= 1000000000000000),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_failure_shape    CHECK ((status = 'FAILED')    = (failure_code IS NOT NULL)),
    CONSTRAINT transfers_settled_shape    CHECK ((status = 'PROCESSED') = (settled_at   IS NOT NULL))
);

CREATE INDEX transfers_from_wallet_created_idx ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_created_idx   ON transfers (to_wallet_id,   created_at DESC);
-- Partial index: supports a future reconciliation sweeper over stuck PENDING rows
-- without carrying the cost of indexing every settled transfer.
CREATE INDEX transfers_pending_idx ON transfers (created_at) WHERE status = 'PENDING';

CREATE TABLE ledger_entries (
    id            BIGSERIAL         PRIMARY KEY,
    transfer_id   UUID              NOT NULL REFERENCES transfers (id) ON DELETE RESTRICT,
    wallet_id     TEXT              NOT NULL REFERENCES wallets   (id) ON DELETE RESTRICT,
    entry_type    ledger_entry_type NOT NULL,
    amount_minor  BIGINT            NOT NULL,
    currency      CHAR(3)           NOT NULL,
    balance_after BIGINT            NOT NULL,
    created_at    TIMESTAMPTZ       NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_amount_positive CHECK (amount_minor > 0),
    -- Guarantees at most one DEBIT and at most one CREDIT per transfer; combined with
    -- both rows being written in the same transaction as the PROCESSED transition,
    -- this yields exactly two balanced entries per processed transfer.
    CONSTRAINT ledger_entries_one_per_side UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id DESC);

-- ---------------------------------------------------------------------------
-- Integrity triggers
-- ---------------------------------------------------------------------------

-- Transfers: core fields are immutable, and the only legal transitions are
-- PENDING -> PROCESSED and PENDING -> FAILED.
CREATE FUNCTION enforce_transfer_invariants() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id              <> OLD.id
    OR NEW.idempotency_key <> OLD.idempotency_key
    OR NEW.request_hash    <> OLD.request_hash
    OR NEW.from_wallet_id  <> OLD.from_wallet_id
    OR NEW.to_wallet_id    <> OLD.to_wallet_id
    OR NEW.amount_minor    <> OLD.amount_minor
    OR NEW.currency        <> OLD.currency THEN
        RAISE EXCEPTION 'transfer % has immutable columns', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status <> OLD.status AND OLD.status <> 'PENDING' THEN
        RAISE EXCEPTION 'illegal transfer state transition % -> % on %',
            OLD.status, NEW.status, OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER transfers_enforce_invariants
    BEFORE UPDATE ON transfers
    FOR EACH ROW EXECUTE FUNCTION enforce_transfer_invariants();

-- Ledger: append-only. A financial ledger that can be rewritten is not a ledger.
CREATE FUNCTION reject_ledger_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only (attempted %)', TG_OP
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
```

`0002_seed_system_wallets.up.sql`:

```sql
INSERT INTO wallets (id, kind, currency, balance_minor)
VALUES ('treasury_inr', 'SYSTEM', 'INR', 0)
ON CONFLICT (id) DO NOTHING;
```

Write matching `.down.sql` files (reverse order drops).

### 4.3 Migrator (`internal/postgres/migrate.go`)

- `//go:embed migrations/*.sql`
- `schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`
- Take `pg_advisory_lock(<constant int64>)` for the duration so concurrent process starts can't race; release in a `defer`.
- Apply each unapplied `*.up.sql` in ascending version order, **each in its own transaction**, inserting its `schema_migrations` row in the same transaction.
- Exported: `Migrate(ctx, pool) error`. Called from `main.go` on startup and from the integration `TestMain`.
- Down migrations are shipped for completeness but are not applied programmatically; note that in the file header.

---

## 5. The transfer algorithm — implement exactly this

This is the heart of the submission. Read it twice before writing code.

### 5.1 Locking model and why (must appear in `docs/design.md`)

- **Isolation level: READ COMMITTED** (Postgres default). Sufficient because *every* read that feeds a write is taken under an explicit row lock. We never make a decision from an unlocked aggregate, so there is no write-skew or phantom exposure, and we avoid the retry loops `SERIALIZABLE` would force on every transfer.
- **Lock type: `SELECT ... FOR NO KEY UPDATE`, not `FOR UPDATE`.** This is deliberate and you must explain it:
  > Inserting a `transfers` row takes a `FOR KEY SHARE` lock on both referenced wallet rows to validate the foreign keys. `FOR UPDATE` conflicts with `FOR KEY SHARE`, so two transactions that had already inserted their transfer rows could deadlock while upgrading. `FOR NO KEY UPDATE` does not conflict with `FOR KEY SHARE`, still conflicts with itself and with the `UPDATE`s we issue against `balance_minor`, and is exactly the strength a non-key column update needs. We never delete wallets or mutate their primary key, so nothing requires the stronger mode.
- **Deterministic lock ordering.** Both wallets are locked in **ascending `id` order, in two separate statements**. Two statements rather than one `WHERE id IN (...) ORDER BY id FOR NO KEY UPDATE`, because Postgres does not guarantee that row locks are acquired in the sorted output order — the plan may lock before the sort. Two statements make the order provable. This is what makes concurrent `A→B` and `B→A` transfers deadlock-free, and there is a test for it.
- **Wallets are locked before the transfer row is inserted**, so the foreign-key `KEY SHARE` locks are already subsumed by locks this transaction holds.
- **`SET LOCAL lock_timeout = '3s'`** at the start of every write transaction; `statement_timeout` is set on the pool via the connection string. A lock timeout (`55P03`) surfaces as `503 RESOURCE_BUSY` with `Retry-After: 1`, never as a 500.
- **Deadlock/serialization retry:** the transaction runner retries on SQLSTATE `40001` and `40P01` up to 3 attempts with jittered backoff (~10ms, ~40ms). Safe because the whole operation is one transaction and idempotent by construction. Include it, and unit-test the runner's retry behaviour with a fake that fails twice then succeeds.

### 5.2 `TransferService.Create` — pseudocode

```
Create(ctx, cmd) -> (Transfer, Outcome, error)

  # ---- 0. Validation (pure domain, no I/O) -------------------------------
  validate cmd: idempotencyKey non-empty & <=255 printable ASCII
                fromWalletId/toWalletId well-formed and different
                amount > 0 and <= MaxAmountMinor
  on failure -> domain.ValidationError            # 400, no transfer row

  fingerprint = sha256hex("v1|" + from + "|" + to + "|" + amount + "|" + currencyOrEmpty)

  # ---- 1. Fast path: replay without touching any lock --------------------
  existing, found = transfers.FindByIdempotencyKey(ctx, key)     # no transaction
  if found:
      return replay(existing, fingerprint)                       # see 5.3

  # ---- 2. Slow path: one transaction, one outcome ------------------------
  uow.Do(ctx, func(tx):
      # 2a. Lock both wallets, ascending id order, two statements.
      first, second = sortAsc(from, to)
      wA = wallets.GetForUpdate(tx, first)     # FOR NO KEY UPDATE
      wB = wallets.GetForUpdate(tx, second)
      if either missing -> domain.ErrWalletNotFound              # 404, no transfer row

      # 2b. Currency rules, before any row is claimed.
      if from.currency != to.currency -> domain.ErrCurrencyMismatch      # 409
      if cmd.currency set and != wallets' currency -> ErrCurrencyMismatch # 409

      # 2c. Claim the idempotency key. INSERT ... ON CONFLICT DO NOTHING
      #     performs a speculative insert: if a concurrent transaction holds the
      #     same key uncommitted, this waits for it, then reports the conflict if
      #     it committed, or inserts if it rolled back. That is exactly the
      #     in-flight deduplication we want, with no extra state.
      transfer = NewPendingTransfer(uuidV7, key, fingerprint, from, to, amount, currency)
      inserted = transfers.InsertIfAbsent(tx, transfer)
      if !inserted:
          existing = transfers.FindByIdempotencyKey(tx, key)     # visible now
          return replay(existing, fingerprint)

      # 2d. Settle or fail — both are terminal, both commit.
      if source is USER and source.balance < amount:
          transfers.MarkFailed(tx, transfer.id, INSUFFICIENT_FUNDS, msg)   # guarded UPDATE
          return transfer(FAILED)                                # 422, committed

      wallets.Debit(tx, from, amount)    # UPDATE ... WHERE id=$1 AND (kind='SYSTEM' OR balance_minor >= $2)
      wallets.Credit(tx, to, amount)     #   -> rows affected must be 1, else fail the tx loudly
      ledger.InsertPair(tx, debitEntry(balanceAfter), creditEntry(balanceAfter))
      transfers.MarkProcessed(tx, transfer.id)   # UPDATE ... WHERE id=$1 AND status='PENDING'
      # every guarded UPDATE asserts RowsAffected() == 1; a 0 means an invariant
      # was violated -> return an error and let the transaction roll back.
      return transfer(PROCESSED)                                 # 201
  )
```

### 5.3 Replay semantics

```
replay(existing, fingerprint):
    if existing.request_hash != fingerprint -> domain.ErrIdempotencyKeyReuse   # 409, nothing mutated
    if existing.status == PENDING           -> domain.ErrTransferInProgress    # 409
    return existing, OutcomeReplayed
```

A replay **must return the byte-identical body and the identical HTTP status code as the original response**, plus the header `Idempotent-Replay: true`. Never 200-on-replay-of-201: `ASSIGNMENT.md` says "return the original result", and the header is how a client distinguishes the two.

### 5.4 Which failures are persisted, and why

This distinction is a graded idea — state it explicitly in `docs/design.md`:

| Failure | Persisted? | HTTP | Rationale |
|---|---|---|---|
| Validation error, wallet not found, currency mismatch, key reuse | **No** | 400 / 404 / 409 | Rejected before the key is claimed. These are *deterministic* — a retry recomputes the same answer, so they are idempotent without storage. |
| Insufficient funds | **Yes — `FAILED`, committed** | 422 | A legitimate attempt that failed at settlement. Committing it makes the outcome stable: a retry with the same key returns the same failure instead of possibly succeeding later, which is what "exactly once" has to mean for a client that is retrying blindly. |
| Infrastructure error (connection loss, timeout, crash mid-transaction) | **No** — transaction rolls back | 503 / 500 | Nothing is left behind, not even a `PENDING` row. The client retries the same key and gets a genuinely fresh attempt. |

Also document: because the entire lifecycle is one transaction, a committed `PENDING` row is not reachable today. `PENDING` is modelled anyway because it is the state the row is claimed in, it is what the guarded transition `WHERE status = 'PENDING'` tests against, and it is the extension point for an asynchronous settlement path. Say this honestly rather than pretending the state machine is more distributed than it is, and note the migration path (commit `PENDING` in transaction 1, settle in transaction 2 driven by an outbox + sweeper) and when it would be warranted (settlement crossing a service or network boundary).

---

## 6. HTTP API contract

Write this up in `docs/api.md` with a `curl` example per endpoint.

### 6.1 Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/transfers` | Create a transfer (idempotent). **Required.** |
| `GET` | `/transfers/{id}` | Fetch a transfer. |
| `POST` | `/wallets` | Create a USER wallet at zero balance. |
| `GET` | `/wallets/{id}` | Balance (optional enhancement in the assignment). |
| `GET` | `/wallets/{id}/entries?limit=&beforeId=` | Ledger history, keyset paginated (optional enhancement). |
| `GET` | `/healthz` | Liveness. No DB call. |
| `GET` | `/readyz` | Readiness. Pings the pool. |
| `GET` | `/metrics` | Prometheus (stretch). |

### 6.2 `POST /transfers`

**The assignment's example request must work verbatim.** This exact body must produce a transfer:

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

- `currency` is an **optional** field. Omitted → inherited from the wallets. Present → must equal both wallets' currency, else `409 CURRENCY_MISMATCH`.
- `idempotencyKey` may alternatively be supplied via the `Idempotency-Key` header. If both are present and differ → `400 VALIDATION_ERROR`. If neither is present → `400 VALIDATION_ERROR` (the service refuses non-idempotent writes rather than silently allowing them — say so in the docs).
- Decode with `json.Decoder` + `DisallowUnknownFields()` + `UseNumber()`, then convert `amount` via `json.Number.Int64()`. This rejects `{"amount": 100.5}`, `{"amount": "100"}`, and typo'd fields instead of coercing them. Cap the body with `http.MaxBytesReader` (64 KiB).

### 6.3 Response bodies

**If the request produced a transfer, the body is the transfer resource.** The HTTP status says whether it settled. The `{"error": ...}` envelope is only used when no transfer exists. Document this rule in one sentence in `docs/api.md`.

Transfer resource:

```json
{
  "id": "0190d1e7-...-uuidv7",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "currency": "INR",
  "status": "PROCESSED",
  "failureCode": null,
  "failureMessage": null,
  "createdAt": "2026-08-12T10:00:00.123456Z",
  "settledAt": "2026-08-12T10:00:00.234567Z"
}
```

Error envelope:

```json
{ "error": { "code": "VALIDATION_ERROR", "message": "amount must be greater than zero", "field": "amount" } }
```

### 6.4 Status code map — implement as a single table-driven mapper in `internal/httpapi/errors.go`

| Condition | Status | `code` | Body |
|---|---|---|---|
| Transfer created and PROCESSED | `201` | — | transfer |
| Replay of a PROCESSED transfer | `201` + `Idempotent-Replay: true` | — | identical transfer |
| Transfer created and FAILED (insufficient funds) | `422` | `failureCode: INSUFFICIENT_FUNDS` on the resource | transfer |
| Replay of a FAILED transfer | `422` + `Idempotent-Replay: true` | as above | identical transfer |
| Malformed JSON / unknown field / bad types | `400` | `MALFORMED_REQUEST` | error |
| Field validation failure | `400` | `VALIDATION_ERROR` | error |
| Source or destination wallet missing | `404` | `WALLET_NOT_FOUND` | error |
| Transfer id not found | `404` | `TRANSFER_NOT_FOUND` | error |
| Same key, different payload | `409` | `IDEMPOTENCY_KEY_REUSE` | error |
| Wallet currencies differ / requested currency differs | `409` | `CURRENCY_MISMATCH` | error |
| Replay observed a PENDING transfer | `409` | `TRANSFER_IN_PROGRESS` | error |
| `POST /wallets` on an existing id | `409` | `WALLET_ALREADY_EXISTS` | error |
| Lock timeout (`55P03`) / statement timeout (`57014`) | `503` + `Retry-After: 1` | `RESOURCE_BUSY` | error |
| Anything else | `500` | `INTERNAL_ERROR` | error (never leak driver/SQL text; log the detail with the request id) |

The handler must never inspect `*pgconn.PgError`. Postgres codes are translated to domain errors in `internal/postgres/errors.go`; the HTTP layer switches on domain errors with `errors.Is`/`errors.As` only. This is a specific thing `copilot-instructions.md` looks for.

---

## 7. Layering rules (graded directly)

- `internal/domain` imports **nothing** from `pgx`, `net/http`, or `internal/*` infrastructure. Entities, value objects, status transitions, validation, sentinel errors. Pure and unit-testable with no fixtures.
- `internal/service` owns business logic, orchestration, idempotency policy, and transactional boundaries. It depends only on interfaces **defined in `internal/service/ports.go`** (consumer-defined interfaces, idiomatic Go) — never on `internal/postgres`.
- `internal/postgres` implements those interfaces. It contains SQL and error translation and **no workflow decisions** — no "if insufficient funds then…" logic lives here.
- `internal/httpapi` does decode → validate shape → call service → map result/error → encode. **No business rules, no SQL.** A handler body should be ~20 lines.
- The transaction boundary is owned by the service via `UnitOfWork`, so the service decides what is atomic, while the mechanics of BEGIN/COMMIT/retry live in `internal/postgres`.

Ports sketch:

```go
// internal/service/ports.go
type Repos interface {
    Wallets()   WalletRepository
    Transfers() TransferRepository
    Ledger()    LedgerRepository
}

// UnitOfWork runs fn inside a single database transaction, retrying on
// serialization failures and deadlocks. fn must be side-effect free outside tx.
type UnitOfWork interface {
    Do(ctx context.Context, fn func(ctx context.Context, r Repos) error) error
    Repos // read-only, non-transactional access for the replay fast path and queries
}

type WalletRepository interface {
    Get(ctx context.Context, id string) (domain.Wallet, error)
    GetForUpdate(ctx context.Context, id string) (domain.Wallet, error) // FOR NO KEY UPDATE
    Create(ctx context.Context, w domain.Wallet) error
    ApplyDelta(ctx context.Context, id string, delta int64) (newBalance int64, err error)
}

type TransferRepository interface {
    InsertIfAbsent(ctx context.Context, t domain.Transfer) (inserted bool, err error)
    FindByID(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
    FindByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error)
    MarkProcessed(ctx context.Context, id uuid.UUID, at time.Time) error
    MarkFailed(ctx context.Context, id uuid.UUID, code, message string) error
}

type LedgerRepository interface {
    InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error
    ListByWallet(ctx context.Context, walletID string, limit int, beforeID *int64) ([]domain.LedgerEntry, error)
}
```

`ApplyDelta` runs the guarded single-statement update and returns the new balance, which feeds `balance_after`:

```sql
UPDATE wallets
   SET balance_minor = balance_minor + $2, updated_at = now()
 WHERE id = $1
   AND (kind = 'SYSTEM' OR balance_minor + $2 >= 0)
RETURNING balance_minor;
```

No rows returned → the guard rejected it → return `domain.ErrInsufficientFunds` and let the caller decide. (The service checks the balance under lock first; this is the second line of defence, and the `CHECK` constraint is the third.)

---

## 8. Testing plan

Discipline: **write the test first, watch it fail, make it pass, then tidy** (the assignment's Red / Blue / Green). Mention concrete examples of this in the PR description — e.g. that the double-spend test was written and observed failing against a pre-lock implementation.

Use `-race` everywhere. Behavioural assertions only: assert on balances, ledger rows, status codes and bodies — never on how many SQL statements ran.

### 8.1 Unit — `internal/domain` (no I/O)

- Table-driven state machine: every `(from, to)` pair in `{PENDING, PROCESSED, FAILED}²` with expected allow/deny.
- Amount validation: zero, negative, above max, overflow on credit.
- Fingerprint: stable across field order, differs when any field differs.
- Wallet debit guard: USER cannot go negative, SYSTEM can.

### 8.2 Unit — `internal/service` (fakes, no DB)

`fakes_test.go` provides an in-memory `UnitOfWork` with a map-backed store, a unique-key check, and a hook to inject errors. Cover:

- happy path → PROCESSED, two entries, balances moved, `OutcomeCreated`
- duplicate key, same payload → `OutcomeReplayed`, no second transfer, balances unchanged
- duplicate key, different payload → `ErrIdempotencyKeyReuse`, nothing mutated
- insufficient funds → transfer persisted as FAILED with `INSUFFICIENT_FUNDS`, zero ledger entries, balances unchanged
- unknown wallet → `ErrWalletNotFound`, no transfer created
- currency mismatch → `ErrCurrencyMismatch`, no transfer created
- same source and destination → `ValidationError`
- transaction runner: fails with `40P01` twice then succeeds → one logical transfer, retry observed

### 8.3 Unit — `internal/httpapi` (httptest + fake service)

- the assignment's exact example body → `201`
- every row of the status-code map in §6.4
- malformed JSON, unknown field, `amount: 100.5`, `amount: "100"`, missing key, oversized body
- replay sets `Idempotent-Replay: true` and returns the identical body/status
- 500 path does not leak internal error text

### 8.4 Integration — `test/integration` (requires `TEST_DATABASE_URL`)

`main_test.go` connects, runs migrations once, and exposes `resetDB(t)` which runs
`TRUNCATE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE` and re-seeds the treasury. DB-touching tests do **not** use `t.Parallel()`. Every test builds the real router over the real pool via `httptest.NewServer`.

Required cases:

1. **Happy path.** Fund `wallet_1`, transfer 100 → `201`, both balances correct, exactly 2 ledger entries, one DEBIT + one CREDIT, equal amounts, correct `balance_after` on each, status PROCESSED, `settled_at` set.
2. **Sequential idempotency.** Same request twice → one transfer row, two ledger entries total, identical bodies, second has the replay header, balances moved once.
3. **Concurrent idempotency.** 25 goroutines, same key, same payload, fired simultaneously → exactly one `transfers` row, exactly two ledger entries, exactly one response without the replay header, every response body identical, balance moved exactly once.
4. **Double spend.** Wallet funded with 100; 10 concurrent transfers of 30 with distinct keys → exactly 3 succeed (`201`), 7 fail (`422 INSUFFICIENT_FUNDS`), final balance 10, ledger sum matches, no negative balance anywhere.
5. **Bidirectional deadlock resistance.** 30 goroutines transferring `A→B` and 30 transferring `B→A` concurrently, all distinct keys → no request returns `500`, all invariants hold, and the combined ledger balances. (Retries may occur; the point is the client never sees a deadlock.)
6. **Insufficient funds persists.** Assert the FAILED row exists, has `failure_code = 'INSUFFICIENT_FUNDS'`, has zero ledger entries, and that a replay of the same key returns the identical `422` body.
7. **Key reuse with a different amount** → `409 IDEMPOTENCY_KEY_REUSE`, and the original transfer is untouched.
8. **DB-level guards** (`internal/postgres/schema_integration_test.go`): direct SQL `UPDATE transfers SET status='FAILED' WHERE status='PROCESSED'` is rejected; `UPDATE`/`DELETE` on `ledger_entries` is rejected; inserting a second DEBIT for one transfer violates the unique constraint; driving a USER wallet negative violates the check constraint.
9. **Rollback leaves nothing.** Open a transaction, insert a PENDING transfer, roll back; then POST the same key over HTTP and get a normal `201`.
10. **Global invariants** — a helper `assertInvariants(t, ctx, pool)` called at the end of *every* integration test:
    - `SELECT SUM(balance_minor) FROM wallets` = `0`
    - `SUM(CASE WHEN entry_type='CREDIT' THEN amount_minor ELSE -amount_minor END) FROM ledger_entries` = `0`
    - every wallet's `balance_minor` equals its ledger sum
    - no `PROCESSED` transfer has anything other than exactly one DEBIT and one CREDIT
    - no `FAILED` transfer has any ledger entries
    - no USER wallet has a negative balance

Case 10 is the highest-value test in the suite. Write it first and call it everywhere.

---

## 9. Observability, config, and operations

- **Config** (`internal/config`): `DATABASE_URL` (required), `HTTP_ADDR` (`:8080`), `LOG_LEVEL` (`info`), `SHUTDOWN_TIMEOUT` (`10s`), `DB_MAX_CONNS` (`10`), `DB_STATEMENT_TIMEOUT` (`5s`), `DB_LOCK_TIMEOUT` (`3s`). Validate on load and fail fast with a clear message. Ship `.env.example`.
- **Logging**: `log/slog` JSON. Middleware assigns a request id (accepting an inbound `X-Request-Id`), puts a scoped logger in the context, and logs one line per request with method, route pattern, status, duration_ms, request_id. The transfer service adds `idempotency_key`, `transfer_id`, `outcome` (`created` / `replayed` / `failed`). **Never log full request bodies or wallet balances at info level.**
- **Panic recovery** middleware → `500 INTERNAL_ERROR` + an error log with the stack.
- **Graceful shutdown**: `signal.NotifyContext` on SIGINT/SIGTERM → `server.Shutdown` with the configured timeout → close the pool.
- **Metrics (stretch, drop it if anything else is unfinished)**: `transfers_total{outcome}`, `transfer_duration_seconds` histogram, `idempotent_replays_total`, `http_requests_total{route,status}`.

---

## 10. Repo furniture

**`Makefile`** — targets with `##` help comments in the owner's style, `DOCKER_CONTEXT ?= desktop-linux` at the top:
`help`, `build`, `run`, `fmt`, `vet`, `lint`, `test-unit`, `test`, `test-integration`, `cover`, `db-up`, `db-down`, `db-psql`, `migrate`, `tidy`.
`make test` must start the DB, wait for `pg_isready`, export `TEST_DATABASE_URL`, and run `go test ./... -race -count=1`.

**`docker-compose.yml`** — one `postgres:16-alpine` service, healthcheck, named volume, port `${POSTGRES_PORT:-5440}:5432` (5440 avoids collisions with the owner's ridego databases on 5433–5436).

**`Dockerfile`** — multi-stage, distroless or alpine final, non-root user, `CGO_ENABLED=0`.

**`.golangci.yml`** — **v1 schema** (no `version: 2` key). Enable: `govet, errcheck, staticcheck, unused, ineffassign, revive, gocritic, gosec, misspell, bodyclose, rowserrcheck, sqlclosecheck, noctx, errorlint, gocyclo (min-complexity 15)`. Exclude `gosec`/`gocyclo` from `_test.go`. The code must pass cleanly — do not add `//nolint` unless you also write the reason.

**`.github/workflows/solution-ci.yml`** — additive; **do not modify `ci.yml` or `sonarqube.yml`** (they are the grader's files and pin a self-hosted runner). New workflow: `runs-on: ubuntu-latest`, `services: postgres:16`, Go 1.24, `gofmt -l` check, `golangci-lint v1.64.8`, `go test ./... -race -count=1` with `TEST_DATABASE_URL` pointed at the service container. This proves the whole suite — including concurrency tests — runs in CI.

**`README.md`** — rewrite as the solution README, keeping a short note that the repo started as an assignment template. Sections: what this is, architecture diagram (ASCII, layer boxes), quickstart (`make db-up && make run`), a copy-pasteable curl walkthrough (create two wallets → fund from treasury → transfer → replay the same key → observe the identical response → check balances and ledger), how to test, configuration table, and a link to `docs/design.md`. Also document the repository-variable values the template CI expects:
`LINT_CMD=golangci-lint run ./...`, `FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"`, `TEST_CMD=go test ./... -race -count=1`.

---

## 11. Documentation deliverables

### `docs/design.md` — write this FIRST, before any Go code

`ASSIGNMENT.md` mandates a documentation-first workflow and names the sections. Use exactly these headings:

1. Problem statement
2. Scope and non-goals
3. Domain model and schema (include the ER relationships and every constraint with its purpose)
4. API contract (link to `docs/api.md`)
5. Side effects
6. Idempotency behaviour (include the "no separate `idempotency_records` table" paragraph from §4.1)
7. Concurrency and consistency (isolation level, `FOR NO KEY UPDATE` rationale, lock ordering, the FK `KEY SHARE` deadlock this avoids, the retry policy)
8. Failure modes (the table from §5.4)
9. Retry behaviour
10. Observability
11. Testing strategy
12. Alternatives considered and rejected — at minimum: separate idempotency table; balances derived purely from the ledger (needs `SERIALIZABLE` or a wallet lock anyway); optimistic locking with a `version` column; `SERIALIZABLE` with retry; two-phase settlement with an outbox and a PENDING sweeper. One short paragraph each: what it buys, what it costs, when you would switch.
13. Deviations from the original plan (may be empty)

Keep it tight — roughly 400–600 lines total. Prose, tables, and SQL snippets; no filler.

### `docs/ai-usage.md`

`ASSIGNMENT.md` requires disclosure of tool, general usage, and the prompts/transcript. Write it factually:

- **Tool:** Claude Code (CLI). Claude Opus 5 for the architecture and planning pass; Claude Sonnet 5 for implementation.
- **How it is generally used:** design first in prose, encode decisions in a spec, implement against the spec, review the output critically. Explicitly note the human decisions: the `FOR NO KEY UPDATE` lock choice, persisting insufficient-funds as a committed FAILED transfer, and rejecting the separate idempotency table.
- **Prompts:** state that the full planning-and-implementation spec is committed as `prompt.md` in this repo, and list the human prompts verbatim.
- Note that a full session transcript is available on request / at `docs/transcript.md` if the owner exports one, and leave a placeholder line the owner can fill in.

### `docs/pr-description.md`

Fill in every section of `.github/pull_request_template.md` — Summary, AI disclosure (all three required points), Schema Design, Idempotency Strategy, Concurrency Strategy, How to Run, How to Test, Tradeoffs / Assumptions, and the checklist. Tradeoffs to name honestly: single-service scope; no auth/rate limiting; INR-only single-currency transfers (no FX); no partial/reversal flows; `PENDING` currently unreachable after commit; balance stored as a projection rather than derived per read.

---

## 12. Git workflow

Branch: **`solution/dhruv-gupta`** off `main` (README asks for `solution/<your-name>`).

Commit in this order. Each commit must build; each `feat` commit lands with its tests. Conventional-commit subjects, imperative mood, and a body explaining *why* for the design-bearing commits (`evaluation_guide.md` §6 grades commit quality).

| # | Subject | Contents |
|---|---|---|
| 1 | `docs: add design note and API contract for wallet transfers` | `docs/design.md`, `docs/api.md` |
| 2 | `chore: scaffold go module, lint, make, and compose setup` | `go.mod`, `.golangci.yml`, `Makefile`, `docker-compose.yml`, `Dockerfile`, `.env.example`, `.gitignore` additions |
| 3 | `feat(db): add wallet, transfer and ledger schema with integrity triggers` | migrations + `migrate.go` + `schema_integration_test.go` |
| 4 | `feat(domain): add money, wallet and transfer state machine` | `internal/domain` + its unit tests |
| 5 | `feat(postgres): add repositories with ordered row locking` | repositories, `db.go` (tx runner + retry), `errors.go` |
| 6 | `feat(service): add idempotent transfer settlement` | `ports.go`, `transfer_service.go`, fakes, unit tests |
| 7 | `feat(http): expose transfer and wallet endpoints` | `internal/httpapi` + handler tests + `cmd/walletd/main.go` |
| 8 | `test(integration): cover idempotency, double spend and ledger invariants` | `test/integration/**` |
| 9 | `feat(observability): add structured logging, request ids and health probes` | middleware, `internal/observability`, probes |
| 10 | `ci: run lint, format and tests against postgres` | `.github/workflows/solution-ci.yml` |
| 11 | `docs: add README, AI usage note and PR description` | `README.md`, `docs/ai-usage.md`, `docs/pr-description.md`, `prompt.md` |
| 12 | `feat(observability): expose prometheus metrics` | *stretch — only if everything above is complete and green* |

Before **every** commit: `gofmt -l .` (empty), `go vet ./...`, `go build ./...`, and the relevant tests. **Do not `git push`. Do not open a PR.**

---

## 13. Definition of done

- [ ] `gofmt -l .` prints nothing; `go vet ./...` is clean
- [ ] `golangci-lint run ./...` is clean (if the pinned v1.64.8 refuses to run against the local Go 1.26 toolchain, say so explicitly in your final report rather than quietly skipping it)
- [ ] `make test-unit` passes on a machine with no database
- [ ] `make test` passes with `-race`, including all concurrency tests
- [ ] The assignment's exact example request body produces a transfer
- [ ] The README curl walkthrough works end to end against a fresh `make db-up`
- [ ] Every integration test ends by asserting the global invariants
- [ ] `docs/design.md`, `docs/api.md`, `docs/ai-usage.md`, `docs/pr-description.md` are complete
- [ ] 11–12 clean commits on `solution/dhruv-gupta`, nothing pushed
- [ ] No `TODO`, no commented-out code, no dead exported functions

---

## 14. Anti-goals — do not build these

Authentication/authorization; rate limiting; multi-currency FX; an ORM or query builder (hand-written SQL only); gRPC or a second transport; an events/outbox table or background worker (discuss it in the design doc instead); a web UI; Kubernetes manifests or Terraform; a generics-heavy repository abstraction; interfaces with a single implementation and a single call site outside `service/ports.go`; a custom logging or error framework; caching; soft deletes; `wallets.version` (unused columns are flagged as dead weight — the lock is the concurrency control, and the design doc explains that optimistic locking was considered and rejected); transfer reversals or refunds.

---

## 15. Final report format

When you finish, report in your own words:

1. What you built and the commit list (`git log --oneline main..solution/dhruv-gupta`)
2. Exact commands run and their real results — paste the `go test` summary lines
3. Anything you could **not** verify locally (e.g. golangci-lint version incompatibility, Docker context problems) and why
4. Any deviations from this spec and the reasoning
5. The three things you would do next with another two hours

Do not claim a check passed unless you ran it and saw it pass.
