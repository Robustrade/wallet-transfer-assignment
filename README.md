# Wallet Transfer Service

A small service that moves money between wallets with a double-entry ledger,
idempotent `POST /v1/transfers`, and database-enforced correctness on PostgreSQL.
The only direct dependency is [pgx](https://github.com/jackc/pgx); everything
else is the Go standard library.

## Problem & scope

Build a wallet transfer API on PostgreSQL where:

- `POST /v1/transfers` moves an integer amount between two wallets and records
the movement in a double-entry ledger;
- clients can retry safely: a client-supplied idempotency key makes duplicate
requests replay the original outcome instead of moving money twice;
- concurrent transfers are safe: many wallets see concurrent traffic, and a few
may be extremely hot;
- PostgreSQL is the source of truth: the invariants that must never break are
enforced by the database, not only by application code.

Deliberately out of scope: authentication, multi-currency conversion, fees, and
rate limiting.

## Setup and local development



### Prerequisites

- Go 1.24+
- Docker and Docker Compose
- `make`

The Makefile defaults to the standalone `docker-compose` command. If you use
the Docker Compose plugin instead, prefix commands with
`COMPOSE="docker compose"`.

### 1. Clone and install dependencies

```sh
git clone <your-fork-or-repository-url> wallet
cd wallet
go mod download
```



### 2. Start PostgreSQL

```sh
make db-up
```

PostgreSQL 17 starts in Docker and is exposed at `localhost:5433`.

### 3. Create the schema and demo wallets

```sh
make migrate
make seed
```

The seed command creates:


| Wallet     | Opening balance |
| ---------- | --------------- |
| `wallet_1` | 1000            |
| `wallet_2` | 500             |


Amounts are integer minor units.

### 4. Run the API

```sh
make run
```

The service listens on `http://localhost:8080`.

In another terminal, confirm it is ready:

```sh
curl -s http://localhost:8080/v1/healthz
```



### 5. Make a local transfer

```sh
curl -s -X POST http://localhost:8080/v1/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Then inspect the updated wallet:

```sh
curl -s http://localhost:8080/v1/wallets/wallet_1
```



### Useful local commands

```sh
make test       # run the full real-PostgreSQL test suite
make fmt        # format Go files
make vet        # run Go static analysis
make db-reset   # destroy local data, then migrate and seed again
make db-down    # stop the local PostgreSQL container
```

Use `DATABASE_URL` to point commands at another PostgreSQL instance. Its
default is:

```text
postgres://wallet:wallet@localhost:5433/wallet?sslmode=disable
```



## API

`/v1` is the canonical API prefix. The unversioned endpoints remain supported
as backwards-compatible aliases with identical behavior.


| Method | Path                      | Description                              |
| ------ | ------------------------- | ---------------------------------------- |
| POST   | `/v1/transfers`           | Execute a transfer (idempotent)          |
| GET    | `/v1/transfers/{id}`      | Fetch one transfer                       |
| GET    | `/v1/wallets/{id}`        | Wallet balance                           |
| GET    | `/v1/wallets/{id}/ledger` | Ledger entries for a wallet (`?limit=N`) |
| GET    | `/v1/healthz`             | Readiness probe (checks the database)    |


`GET /v1/wallets/{id}/ledger` returns entries newest first; the page size
defaults to 100 and larger values are clamped to that. Malformed ids, and
malformed or non-positive limits, are 400 `VALIDATION_ERROR`; unknown ids are
404 `WALLET_NOT_FOUND` / `TRANSFER_NOT_FOUND`. Requests that match no route are
404 `NOT_FOUND`; a known path called with an unsupported method is 405
`METHOD_NOT_ALLOWED`, with the permitted methods in the `Allow` header.

### POST /v1/transfers

Request:

```json
{"idempotencyKey":"demo-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}
```

Outcomes — "recorded" means the status and body are stored against the
idempotency key and replayed byte-for-byte on duplicates:


| Status | Code                     | Meaning                                                | Recorded |
| ------ | ------------------------ | ------------------------------------------------------ | -------- |
| 201    | –                        | Transfer PROCESSED; balances and ledger updated        | yes      |
| 404    | `WALLET_NOT_FOUND`       | A wallet does not exist; no transfer created           | yes      |
| 422    | `INSUFFICIENT_FUNDS`     | Transfer FAILED; `transferId` in the error body        | yes      |
| 422    | `BALANCE_OVERFLOW`       | Destination balance cannot represent the credit        | yes      |
| 400    | `VALIDATION_ERROR`       | Malformed or invalid request; the key is not claimed   | no       |
| 409    | `IDEMPOTENCY_KEY_REUSED` | Same key, different payload; rejected                  | no       |
| 503    | `LOCK_TIMEOUT`           | Lock acquisition exceeded its budget; retry same key   | no       |
| 503    | `STATEMENT_TIMEOUT`      | Statement time limit exceeded; retry same key          | no       |
| 503    | `DATABASE_UNAVAILABLE`   | Database unreachable or pool saturated; retry same key | no       |
| 503    | `OUTCOME_UNKNOWN`        | Commit outcome unconfirmed; retry same key             | no       |
| 499    | `CLIENT_CLOSED_REQUEST`  | Client disconnected; this request committed nothing    | no       |
| 500    | `INTERNAL_ERROR`         | Unexpected fault; details are in the server log        | no       |


The first three 503s are definite failures: nothing has been committed.
`OUTCOME_UNKNOWN` means the commit itself is in doubt — it may or may not have
landed. Retries are safe either way: retrying the identical request with the
same key either succeeds fresh or replays a recorded outcome. A 499 means the
caller disconnected before an outcome existed; that request committed nothing
(a disconnect during the commit itself is `OUTCOME_UNKNOWN`, not 499).

## Design



### Architecture

```mermaid
flowchart TB
    Client["Client"]

    subgraph App["Wallet Transfer Service · Go"]
        Main["cmd/api<br/>config · pgxpool · HTTP server"]
        Router["internal/api<br/>routing · strict decoding<br/>error mapping · recovery · request logs"]
        Service["internal/service<br/>validation · idempotency<br/>transfer state machine"]
        Ports["service.Store / service.Tx<br/>storage ports"]
        Store["internal/store/postgres<br/>SQL · transaction handling<br/>timeout/error mapping"]
    end

    subgraph Tools["Operational entrypoints"]
        Migrate["cmd/migrate<br/>embedded migrations"]
        Seed["cmd/seed<br/>demo wallets"]
    end

    Pool["pgx connection pool<br/>MaxConns = 32<br/>5 s bounded acquire"]
    DB[("PostgreSQL 17")]

    Client --> Main
    Main --> Router
    Router --> Service
    Service --> Ports
    Ports --> Store
    Store --> Pool
    Pool --> DB

    Migrate --> DB
    Seed --> DB
```



Handlers stay thin: they decode strictly, call the service, and write back what
the service decided. The service depends on the `Store`/`Tx` interfaces
(`internal/service/ports.go`), never on pgx or on HTTP handlers — its only
`net/http` import is for status-code constants. The store is the only layer
that speaks SQL. Routing uses the standard library's `net/http` method patterns;
logging is `log/slog`; the server shuts down gracefully on SIGINT/SIGTERM.

### Database schema

```mermaid
erDiagram
    WALLETS {
        text id PK "CHECK (id <> '')"
        bigint balance "NOT NULL, CHECK (balance >= 0)"
        char(3) currency "NOT NULL, default USD"
        timestamptz created_at
        timestamptz updated_at
    }

    TRANSFERS {
        uuid id PK
        text from_wallet_id FK
        text to_wallet_id FK
        bigint amount
        text status
        text failure_reason
        timestamptz created_at
        timestamptz updated_at
    }

    LEDGER_ENTRIES {
        bigint id PK
        uuid transfer_id FK
        text wallet_id FK
        text entry_type
        bigint amount FK
        timestamptz created_at
    }

    IDEMPOTENCY_RECORDS {
        text idempotency_key PK
        jsonb request_body
        uuid transfer_id FK
        smallint response_status
        text response_body
        timestamptz created_at
        timestamptz updated_at
    }

    WALLETS ||--o{ TRANSFERS : "from_wallet_id"
    WALLETS ||--o{ TRANSFERS : "to_wallet_id"
    WALLETS ||--o{ LEDGER_ENTRIES : "wallet_id"
    TRANSFERS ||--o{ LEDGER_ENTRIES : "composite FK: (transfer_id, amount)"
    TRANSFERS o|--o{ IDEMPOTENCY_RECORDS : "transfer_id"
```




| Table                 | Purpose                                   | Key pins                                                                                                                                                                                                       |
| --------------------- | ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `wallets`             | Stored balance per wallet                 | `balance >= 0`, non-blank id                                                                                                                                                                                   |
| `transfers`           | One attempted movement; state machine row | amount > 0, distinct wallets, valid status, FAILED ⇔ failure_reason, `UNIQUE (id, amount)` (target of the ledger's composite FK)                                                                               |
| `ledger_entries`      | Double-entry pair per processed transfer  | `(transfer_id, amount)` FK → `transfers (id, amount)` pins each entry to its transfer's amount; `UNIQUE (transfer_id, entry_type)` allows at most one DEBIT and one CREDIT; amount > 0; type ∈ {DEBIT, CREDIT} |
| `idempotency_records` | One row per idempotency key               | PK on the key; response status/body stored together (all-or-nothing); only 201/404/422 can be recorded; 404 rows carry no transfer id                                                                          |


Money is `bigint` minor units — no floats, ever. Migration `0001_init.sql`
carries the full constraint list. The two `transfers_*_wallet_idx` indexes that
0001 created were dropped in `0002_drop_unused_transfer_indexes.sql`: no query
uses them (transfers are read by primary key; the composite FK's unique index
is required by the schema), so they were pure write cost on every transfer
insert. Re-add them with `CREATE INDEX CONCURRENTLY` if an operational access
path appears.

### Balance strategy

Balances are stored on the wallet row and updated *relatively*:
`SET balance = balance - $2 WHERE id = $1 AND balance >= $2`. There is no
read-modify-write anywhere, so lost updates are impossible; the guard clause
decides sufficiency, and the `balance >= 0` CHECK is the last line of defense.
The ledger — not a balance-history column — is the record of how a balance got
where it is.

### Transaction boundary

```mermaid
flowchart TD
    Start["POST /v1/transfers"] --> Decode["Strict JSON decode"]
    Decode --> Valid{"Request valid?"}
    Valid -- "No" --> BadRequest["400 VALIDATION_ERROR<br/>Key is not claimed"]
    Valid -- "Yes" --> Canonical["Canonicalize decoded request"]

    subgraph Tx["One READ COMMITTED PostgreSQL transaction"]
        Begin["BEGIN<br/>SET LOCAL lock_timeout + statement_timeout"]
        Claim["Claim idempotency key<br/>INSERT ... ON CONFLICT DO NOTHING"]
        Won{"Key claimed?"}
        Replay["Read idempotency record<br/>and compare canonical request"]
        Match{"Payload matches?"}
        KeyReuse["409 IDEMPOTENCY_KEY_REUSED<br/>Original record remains intact"]

        Lock["Lock both wallets in ascending ID order<br/>FOR NO KEY UPDATE"]
        Wallets{"Both wallets exist?"}
        NotFound["Complete idempotency record<br/>with recorded 404"]

        Pending["Insert PENDING transfer"]
        Debit["Guarded debit<br/>balance >= amount"]
        Funded{"Debit succeeded?"}
        Failed["Mark transfer FAILED<br/>Complete idempotency with recorded 422"]

        Credit["Credit destination wallet"]
        Ledger["Insert one DEBIT + one CREDIT"]
        Processed["Mark transfer PROCESSED<br/>Complete idempotency with recorded 201"]

        Commit["COMMIT"]
    end

    Canonical --> Begin --> Claim --> Won
    Won -- "No; waits for winner if needed" --> Replay --> Match
    Match -- "Yes" --> Commit
    Match -- "No" --> KeyReuse --> Rollback

    Won -- "Yes" --> Lock --> Wallets
    Wallets -- "No" --> NotFound --> Commit
    Wallets -- "Yes" --> Pending --> Debit --> Funded
    Funded -- "No" --> Failed --> Commit
    Funded -- "Yes" --> Credit --> Ledger --> Processed --> Commit

    Commit --> Result["Return fresh or replayed<br/>201 / 404 / 422 response"]

    Claim -. "lock, statement, database, or app error" .-> Rollback["ROLLBACK<br/>No durable effects"]
    Lock -. "timeout or error" .-> Rollback
    Pending -. "error" .-> Rollback
    Debit -. "error" .-> Rollback
    Credit -. "error" .-> Rollback
    Ledger -. "error" .-> Rollback
    Processed -. "error" .-> Rollback
    Rollback --> Error["Mapped 503 or 500"]

    Commit -. "commit outcome unknown" .-> Resolve["Fresh-connection read of<br/>idempotency record"]
    Resolve --> Resolved{"Complete matching<br/>record found?"}
    Resolved -- "Yes" --> Result
    Resolved -- "No" --> Unknown["503 OUTCOME_UNKNOWN<br/>Retry same key"]
```



One transaction per transfer attempt:

1. claim the idempotency key (`INSERT … ON CONFLICT DO NOTHING`),
2. lock both wallets in ascending id order (one statement),
3. insert the transfer as PENDING,
4. debit (guarded), credit, write the ledger pair — or mark the transfer FAILED
  with a reason,
5. record the outcome against the key, COMMIT, then respond.

The response is only written after the commit, so an outcome can never be
observed without its effects. Any failure inside the transaction rolls back
everything; a client never sees a partial transfer. READ COMMITTED is required:
under REPEATABLE READ the claim path would surface serialization failures
(40001) instead of clean duplicate detection.

### Concurrency & locking

- Wallets are locked by a single statement,
`SELECT … WHERE id = ANY($1) ORDER BY id FOR NO KEY UPDATE`, which acquires
row locks in ascending id order (the plan is literally `LockRows → Sort(id)`),
making deadlock between transfers impossible. A raw-SQL unordered control
test produces 40P01 to pin the counterexample.
- Same-key duplicates queue at the idempotency claim before touching any wallet.
- Bounded waits everywhere: `lock_timeout` (default 2000 ms) caps each lock
acquisition attempt (a transfer's two-row lock statement can therefore wait
once per wallet), `statement_timeout` (5000 ms) caps any single statement,
and every pool acquire — inside `InTx` and on the read paths — is bounded at
5 s. On expiry the request ends as a clean, retry-safe 503 with nothing
committed — under sustained overload the service sheds load instead of
queueing unboundedly.
- Hot wallets: throughput per wallet is bounded by row-lock serialization (one
transfer per wallet at a time; ~400-500 tx/s into a single hot pair measured
on a Docker Desktop dev machine). Serving 10k tx/s concentrated into a few
wallets would require a different write model (netting/batching or sharded
balances), which is deliberately out of scope.



### Pool sizing

`MaxConns` is 32 per instance, picked from a measured sweep (15 s windows,
64 concurrent clients, real PostgreSQL, one hot pair):


| `MaxConns` | `GET /wallets/{id}`   | replay (same key)   | hot-pair transfers |
| ---------- | --------------------- | ------------------- | ------------------ |
| 8          | 12.9k rps, p50 4.8 ms | 3.0k rps, p50 20 ms | 475 rps, 0×503     |
| 16         | 17.9k rps, p50 3.4 ms | 3.8k rps, p50 16 ms | 359 rps, 1×503     |
| 32         | 21.7k rps, p50 2.7 ms | 5.1k rps, p50 12 ms | 354 rps, 15×503    |
| 64         | 22.1k rps, p50 2.7 ms | 5.3k rps, p50 12 ms | 286 rps, 0×503     |


Reads and replays scale to 32 connections and then plateau (+2-4% at 64, where
the 8-vCPU server is the bottleneck, not the pool). Transfers are bounded by
row-lock serialization on the hot pair instead: more connections only deepen
the lock queue, and the 503s above are `statement_timeout` expiries in that
queue — all retry-safe. `MinConns` stays 0: a cold start measured p99 3.1 ms
versus 2.7 ms warm (only the first-burst connect storm shows a 65 ms max), so
pre-warming buys nothing here. Keep `instances × MaxConns` well below the
server's `max_connections` (100 by default); past that fan-in, PgBouncer goes
in front (see Limitations).

### Idempotency

Claim → work → record, all in the same transaction; the key's primary key makes
double-commits impossible. Later duplicates replay the stored status and body
byte-for-byte. Payload comparison happens inside PostgreSQL as `jsonb`, against
a canonicalized request (fixed field order, built from decoded values), so
cosmetic JSON differences compare equal and semantic differences never do.

Intentional semantics — reading these wrong will look like bugs:

- same key + different payload → 409, and the key is *not* burned; the original
record stays replayable;
- 400 (validation) never claims a key; 409 never claims a key; the 503s for
lock/statement/transport failures leave no trace at all, while
`OUTCOME_UNKNOWN` may have committed — the same-key retry converges either way;
- 404 outcomes are recorded without creating a transfer row;
- a committed key is always complete. A conflict that finds an incomplete
record is treated as an internal error (500) and logged loudly — it is a
tripwire for a state that is unreachable by design;
- if COMMIT itself fails, the outcome is resolved by reading the record back on
a fresh connection; if that cannot confirm the result, the client gets 503
`OUTCOME_UNKNOWN`, and a retry with the same key converges (replay or fresh
claim).



### Transfer state machine

`PENDING → PROCESSED | FAILED`; terminal states never change. Transitions run
as guarded updates (`WHERE status = 'PENDING'` plus a rowcount assertion), and
the schema pins the same rules in CHECKs. Because everything commits at once,
transfers are never externally observable in PENDING: clients only ever see
PROCESSED (201) or FAILED (422).

### Ledger consistency

Every PROCESSED transfer has exactly one DEBIT (from the source wallet) and one
CREDIT (to the destination), both equal to the transfer's amount. The schema
enforces the upper bound and the amount pinning structurally; the transaction
writes both sides from the same value in one statement; tests verify the rest
(see Integrity audit queries). FAILED transfers have no ledger entries, and a
wallet's balance delta always equals its ledger net, because both are written
in the same transaction from the same amount.

### Failure handling

Every write step can fail without leaving a trace — the failure suite injects a
fault into each one and asserts that with a whole-database consistency oracle.
Panics are recovered per-request and returned as 500 without taking the server
down. Database outages surface as fast 503s, and `/healthz` reports 503 so
orchestrators stop routing traffic. The full status/code mapping is the table
under API above; the mapping lives in one place (`internal/api/responses.go`).

## Observability

- One structured JSON log line per HTTP request: method, path, status,
`duration_ms` (slog to stdout).
- Business events with identifiers: transfer processed/failed (with transfer
id, wallets, amount), replays, key-reuse warnings, commit-outcome resolutions,
and the incomplete-record tripwire as an error.
- `GET /v1/healthz` is the canonical readiness probe: it pings the database
with a 2 s timeout and answers 503 when unavailable.
- Not implemented: metrics and tracing. At production volume the request log
would need sampling.



## Testing

Tests run against a real PostgreSQL (`wallet_test`, created on first run):
row locking, constraints, and rollback semantics only exist in the database, so
nothing is mocked where database behavior is the subject. Override the test
database with `TEST_DATABASE_URL`. Suites (38 tests):

- `tests/smoke_test.go` — HTTP end-to-end: happy path, key reuse, validation
and unknown wallets, ledger limit validation.
- `tests/concurrency_test.go` — 10 barrier-released race tests through the real
HTTP stack: same-wallet within/over balance, 50-racer saturation, hot
destination, same-key races (identical payloads, different payloads, recorded
failure replayed), in-flight duplicate waits, opposite-direction transfers,
independent parallel transfers.
- `tests/failure_test.go` — 12 fault-injection tests: one-shot failure of every
write step, database outage containment, and the commit-in-doubt outcomes
(landed, never landed, and landed while the client was already gone), each
verified against a whole-database oracle.
- `internal/store/postgres` — SQL-level: claim conflict/replay, ordered locking
(plus the unordered deadlock control), guarded debit, ledger pinning,
terminal states, write-once completion, raw-SQL constraint pins.
- `internal/api`, `internal/config` — error-mapping table, router 404/405
fallbacks, config parsing.

```sh
make db-up
make test                    # go test ./... -count=1
go test ./tests/... -run Smoke -v   # HTTP end-to-end only
```

Test packages share one database and are serialized by an advisory lock so they
cannot truncate each other's fixtures. `go vet ./...` is the static analysis,
`gofmt` the formatter (`make fmt`, `make vet`).

### Integrity audit queries

The seed's opening balances are facts *outside* the ledger (see Known
limitations), so the reconciliation takes them as input:

```sql
-- Balance audit: balance must equal opening balance + ledger net.
WITH openings (wallet_id, opening) AS (
  VALUES ('wallet_1', 1000::bigint), ('wallet_2', 500::bigint)
),
net AS (
  SELECT wallet_id,
         sum(CASE entry_type WHEN 'DEBIT' THEN -amount ELSE amount END) AS delta
  FROM ledger_entries GROUP BY wallet_id
)
SELECT w.id, w.balance, o.opening, o.opening + COALESCE(n.delta, 0) AS expected
FROM wallets w
LEFT JOIN openings o ON o.wallet_id = w.id
LEFT JOIN net n ON n.wallet_id = w.id
WHERE w.balance IS DISTINCT FROM o.opening + COALESCE(n.delta, 0);
```

```sql
-- Every PROCESSED transfer has its DEBIT and CREDIT, each equal to its amount.
SELECT t.id
FROM transfers t
LEFT JOIN ledger_entries d ON d.transfer_id = t.id AND d.entry_type = 'DEBIT'
LEFT JOIN ledger_entries c ON c.transfer_id = t.id AND c.entry_type = 'CREDIT'
WHERE t.status = 'PROCESSED'
  AND (d.id IS NULL OR c.id IS NULL OR d.amount <> t.amount OR c.amount <> t.amount);

-- Entries only exist for PROCESSED transfers, on the correct wallet side.
SELECT le.id
FROM ledger_entries le
JOIN transfers t ON t.id = le.transfer_id
WHERE t.status <> 'PROCESSED'
   OR (le.entry_type = 'DEBIT'  AND le.wallet_id <> t.from_wallet_id)
   OR (le.entry_type = 'CREDIT' AND le.wallet_id <> t.to_wallet_id);
```



## Configuration


| Variable          | Default                                                          | Meaning                         |
| ----------------- | ---------------------------------------------------------------- | ------------------------------- |
| `HTTP_ADDR`       | `:8080`                                                          | Listen address                  |
| `DATABASE_URL`    | `postgres://wallet:wallet@localhost:5433/wallet?sslmode=disable` | Connection string               |
| `LOCK_TIMEOUT_MS` | `2000`                                                           | Per lock acquisition attempt    |
| `RUN_MIGRATIONS`  | `false`                                                          | Apply migrations at API startup |




## Assumptions

- Single currency: `currency` is stored and returned, but transfers do not check
that both wallets share it (multi-currency would need an FX policy).
- Amounts are integers in minor units; the max accepted amount is 10^15.
- Idempotency keys and wallet ids are opaque strings up to 255 characters
(keys: no leading/trailing whitespace, no NUL bytes).
- Clients retry 5xx responses with the *same* idempotency key.
- One primary database serves both writes and reads; the database clock is the
only time source.



## Trade-offs

- **One transaction per transfer.** Atomicity and replay-by-construction over
raw throughput; the whole attempt — claim, locks, money movement, outcome
record — is one commit.
- **Claim before lock.** Every request inserts its idempotency row before
touching wallets: one extra insert per transfer buys duplicate safety and
deadlock-free ordering.
- **Bounded waits over unbounded queueing.** Lock/statement/acquire timeouts
convert overload into retry-safe 503s instead of latency creep.
- **Stored response bodies.** Outcomes are persisted so replays are
byte-identical; a little storage per key buys exact replay semantics.
- **Session settings per transaction** (`SET LOCAL`, both timeouts in one
statement) rather than per-connection parameters: self-contained behavior,
at the cost of a single round trip.
- **Dropped the speculative indexes** on `transfers(from_wallet_id)` and
`transfers(to_wallet_id)` (migration `0002`): no query plan ever chose them,
and they were pure write cost on every transfer insert; re-add with
`CREATE INDEX CONCURRENTLY` if an operational access path materializes.



## Known limitations

- **No retention for** `idempotency_records` — it grows unboundedly. A purge
job should delete completed records past their replay window; never delete a
claimed-but-incomplete row (that state is an in-flight request):
  ```sql
  DELETE FROM idempotency_records
  WHERE response_status IS NOT NULL
    AND updated_at < now() - interval '30 days';
  ```
- **Opening balances are outside the ledger.** The seeded wallets start at
1000/500 and that genesis is not itself a ledger entry, so balance == ledger
net holds only relative to opening balances. The audit SQL above takes
openings as input; in a production ledger, genesis would be posted as
entries.
- **Ledger immutability at the row level is not enforced by the schema.** Direct
SQL can still rewrite a `wallet_id` or delete a `ledger_entries` row (production:
use a DB role without UPDATE/DELETE on `ledger_entries`). The schema does enforce
that all database-level invariants are enforced in migration 0005: ledger entries
only attach to PROCESSED transfers (deferred constraint trigger), DEBIT references
the source wallet and CREDIT the destination (BEFORE trigger), wallet IDs cannot
change once ledger entries exist (BEFORE UPDATE trigger), and
at most one DEBIT and one CREDIT per transfer (unique constraint).
- **The incomplete-record conflict is a tripwire, not a recovery path**: 500 +
error log. Unreachable by construction; loud on purpose.
- **Hot-wallet throughput is bounded by row-lock serialization** (~400-500 tx/s
into a single hot pair in the load measurements). Extreme concentration needs
a redesign (netting/batching/sharded balances), not tuning.
- **No PgBouncer in this deployment.** One instance × 32 pool connections sits
far below `max_connections=100`, and the protocol is chatty (11 round trips
per transfer, 5 per replay), so a proxy hop would only add latency.
Transaction-mode pooling would also break pgx's default prepared-statement
cache (`QueryExecModeCacheStatement`) unless PgBouncer ≥ 1.21 runs with
`max_prepared_statements`, or the app switches exec mode. PgBouncer earns its
keep at multi-instance fan-in (instances × pool > `max_connections`), with
churny/serverless clients, or to shrink backend memory — not here.
- No metrics/tracing, no auth, no rate limiting.



## Project layout

```
cmd/
  api/            HTTP server entrypoint (main.go)
  migrate/        Standalone migration runner (main.go)
  seed/           Demo-wallet seeder (main.go)

internal/
  api/            HTTP layer: router, strict decoding, middleware,
                  error mapping, transfers and wallets handlers
  config/         Environment-variable configuration loading
  constants/      Shared constants (limits, timeouts, SQL queries)
  domain/         Core types (Wallet, Transfer, LedgerEntry) and
                  domain error sentinels
  dto/            JSON request/response shapes
  service/        Business logic: validation, idempotency, transfer
                  state machine, replay; storage ports (ports.go)
  store/postgres/ pgx implementations of the service ports:
                    store.go        pool, InTx, error mapping
                    wallets.go      GetWallet, LockWallets, Debit/Credit
                    transfers.go    CreateTransfer, MarkProcessed/Failed, GetTransfer
                    ledger.go       InsertLedgerEntries, ListWalletLedger
                    idempotency.go  ClaimIdempotency, CompleteIdempotency,
                                    GetIdempotencyRecord

migrations/
  0001_init.sql                              Initial schema (wallets, transfers,
                                             ledger_entries, idempotency_records)
  0002_drop_unused_transfer_indexes.sql      Remove speculative indexes
  0003_strengthen_idempotency_outcome_constraint.sql
  0004_enforce_ledger_wallet_roles.sql       BEFORE trigger: DEBIT→source,
                                             CREDIT→destination
  0005_strengthen_invariants.sql             Consolidated database-level
                                             invariants: wallet role enforcement,
                                             state machine, deferred pair
                                             validation, immutability, and
                                             deletion safeguards
  
  embed.go                                   //go:embed *.sql for the runner
  runner.go                                  Advisory-locked migration runner

tests/
  smoke_test.go       HTTP end-to-end happy path, key reuse, validation
  concurrency_test.go 10 barrier-released race tests through the HTTP stack
  failure_test.go     12 fault-injection tests + whole-database oracle
  faultstore_test.go  Fault-injection store wrapper used by failure tests
  doc.go              Package doc
  testutil/
    testutil.go       Real-PostgreSQL test harness: DB creation, migrations,
                      advisory-lock suite serialization, Env helpers
```



## AI usage

How AI was used on this submission — the tool, the staged workflow, my
decisions and approvals, and the observed limitations of AI-generated
suggestions — is documented in [AI_USAGE.md](AI_USAGE.md).
